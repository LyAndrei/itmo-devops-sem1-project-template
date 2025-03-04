package handlers

import (
    "archive/zip"
    "bytes"
    "database/sql"
    "encoding/csv"
    "encoding/json"
    "io"
    "log"
    "math"
    "net/http"
    "path/filepath"
    "strconv"
    "time"
)

// PriceData используется и при чтении CSV, и при выгрузке данных из БД.
type PriceData struct {
    ID        string
    CreatedAt time.Time
    Name      string
    Category  string
    Price     float64
}

// PostResponse описывает JSON-ответ для POST-запроса.
type PostResponse struct {
    TotalItems      int     `json:"total_items"`      // кол-во реально вставленных строк
    TotalCategories int     `json:"total_categories"` // общее кол-во категорий в БД
    TotalPrice      float64 `json:"total_price"`      // суммарная стоимость в БД
}

// PostPrices обрабатывает загрузку ZIP-архива, в котором лежит data.csv.
func PostPrices(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        file, _, err := r.FormFile("file")
        if err != nil {
            log.Printf("Error retrieving file: %v", err)
            http.Error(w, "Failed to retrieve file", http.StatusBadRequest)
            return
        }
        defer file.Close()

        // Считываем ZIP-архив в буфер
        zipBuffer := &bytes.Buffer{}
        if _, err := io.Copy(zipBuffer, file); err != nil {
            log.Printf("Error reading file: %v", err)
            http.Error(w, "Failed to read file", http.StatusInternalServerError)
            return
        }

        // Открываем ZIP-архив из буфера
        zipReader, err := zip.NewReader(bytes.NewReader(zipBuffer.Bytes()), int64(zipBuffer.Len()))
        if err != nil {
            log.Printf("Error opening zip: %v", err)
            http.Error(w, "Invalid zip file", http.StatusBadRequest)
            return
        }

        // Собираем валидные записи из CSV
        var validRecords []PriceData

        for _, zf := range zipReader.File {
            if filepath.Ext(zf.Name) != ".csv" {
                continue
            }
            csvFile, err := zf.Open()
            if err != nil {
                log.Printf("Error opening CSV in zip: %v", err)
                continue
            }
            defer csvFile.Close()

            reader := csv.NewReader(csvFile)

            // Явно пропускаем строку заголовка (id,name,category,price,create_date)
            if _, err := reader.Read(); err != nil {
                log.Printf("Error reading header: %v", err)
                continue
            }
            // Читаем все строки
            for {
                record, err := reader.Read()
                if err == io.EOF {
                    break
                }
                if err != nil {
                    log.Printf("Error reading CSV record: %v", err)
                    continue
                }
                if len(record) < 5 {
                    log.Printf("Invalid record: %v", record)
                    continue
                }

                idStr := record[0]
                name := record[1]
                category := record[2]
                priceStr := record[3]
                dateStr := record[4]

                // Парсим цену
                priceVal, err := strconv.ParseFloat(priceStr, 64)
                if err != nil {
                    log.Printf("Invalid price: %v", priceStr)
                    continue
                }
                // Парсим дату
                createdAt, err := time.Parse("2006-01-02", dateStr)
                if err != nil {
                    log.Printf("Invalid date: %v", dateStr)
                    continue
                }

                validRecords = append(validRecords, PriceData{
                    ID:        idStr,
                    CreatedAt: createdAt,
                    Name:      name,
                    Category:  category,
                    Price:     priceVal,
                })
            }
        }

        // Начинаем транзакцию
        tx, err := db.Begin()
        if err != nil {
            log.Printf("Failed to begin transaction: %v", err)
            http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
            return
        }
        defer func() { _ = tx.Rollback() }()

        // Список для хранения успешно обработанных строк (для расчёта total_items)
        var successfullyProcessed int

        for _, rec := range validRecords {
            _, err := tx.Exec(`
                INSERT INTO prices (id, created_at, name, category, price)
                VALUES ($1, $2, $3, $4, $5)
                ON CONFLICT (id) DO NOTHING
            `, rec.ID, rec.CreatedAt, rec.Name, rec.Category, rec.Price)
            if err != nil {
                log.Printf("DB insert error: %v", err)
                continue
            }
            
            successfullyProcessed++
        }

        var dbCategories int
        var dbTotalPrice float64

        row := tx.QueryRow(`
            SELECT COUNT(DISTINCT category), COALESCE(SUM(price), 0)
            FROM prices
        `)
        if err := row.Scan(&dbCategories, &dbTotalPrice); err != nil {
            log.Printf("Failed to scan totals: %v", err)
            http.Error(w, "Failed to calculate totals", http.StatusInternalServerError)
            return
        }

        if err := tx.Commit(); err != nil {
            log.Printf("Failed to commit transaction: %v", err)
            http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
            return
        }

        resp := PostResponse{
            TotalItems:      successfullyProcessed,
            TotalCategories: dbCategories,
            TotalPrice:      math.Round(dbTotalPrice*100) / 100,
        }

        w.Header().Set("Content-Type", "application/json")
        if err := json.NewEncoder(w).Encode(resp); err != nil {
            log.Printf("Error encoding JSON: %v", err)
        }
    }
}

// GetPrices выгружает все записи из БД в data.csv и возвращает ZIP-архив
func GetPrices(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        rows, err := db.Query(`
            SELECT id, created_at, name, category, price 
            FROM prices
        `)
        
        if err != nil {
            log.Printf("Error querying database: %v", err)
            http.Error(w, "Failed to retrieve data", http.StatusInternalServerError)
            return
        }

        var allPrices []PriceData

        for rows.Next() {
            var (
                idInt     int
                createdAt time.Time
                name      string
                category  string
                priceVal  float64
            )
            if err := rows.Scan(&idInt, &createdAt, &name, &category, &priceVal); err != nil {
                log.Printf("Error scanning row: %v", err)
                continue
            }
            allPrices = append(allPrices, PriceData{
                ID:        strconv.Itoa(idInt),
                CreatedAt: createdAt,
                Name:      name,
                Category:  category,
                Price:     priceVal,
            })
        }
        if rows.Err() != nil {
            log.Printf("Error after rows.Next(): %v", rows.Err())
            http.Error(w, "Failed to read rows", http.StatusInternalServerError)
            return
        }
        rows.Close()

        csvBuffer := &bytes.Buffer{}
        writer := csv.NewWriter(csvBuffer)

        // Заголовок CSV
        writer.Write([]string{"id", "name", "category", "price", "create_date"})

        for _, p := range allPrices {
            record := []string{
                p.ID,
                p.Name,
                p.Category,
                strconv.FormatFloat(p.Price, 'f', 2, 64),
                p.CreatedAt.Format("2006-01-02"),
            }
            writer.Write(record)
        }
        writer.Flush()

        if err := writer.Error(); err != nil {
            log.Printf("Error finalizing CSV: %v", err)
            http.Error(w, "Failed to write CSV", http.StatusInternalServerError)
            return
        }

        // Упаковываем CSV-файл в ZIP-архив
        zipBuffer := &bytes.Buffer{}
        zipWriter := zip.NewWriter(zipBuffer)

        csvFile, err := zipWriter.Create("data.csv")
        if err != nil {
            log.Printf("Error creating file in ZIP: %v", err)
            http.Error(w, "Failed to create ZIP", http.StatusInternalServerError)
            return
        }

        if _, err := csvFile.Write(csvBuffer.Bytes()); err != nil {
            log.Printf("Error writing CSV to ZIP: %v", err)
            http.Error(w, "Failed to write ZIP", http.StatusInternalServerError)
            return
        }

        if err := zipWriter.Close(); err != nil {
            log.Printf("Error closing ZIP writer: %v", err)
            http.Error(w, "Failed to close ZIP", http.StatusInternalServerError)
            return
        }

        // Отправляем ZIP-архив клиенту
        w.Header().Set("Content-Type", "application/zip")
        w.Header().Set("Content-Disposition", "attachment; filename=data.zip")
        w.WriteHeader(http.StatusOK)
        if _, err := w.Write(zipBuffer.Bytes()); err != nil {
            log.Printf("Error sending ZIP file: %v", err)
        }
    }
}
