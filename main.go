package main

import (
    "log"
    "net/http"
    "os"

    "github.com/gorilla/mux"
    "github.com/joho/godotenv"
    "project_sem/handlers"
    "project_sem/models"
)

func main() {
    // Загрузка переменных окружения из файла .env
    if err := godotenv.Load(); err != nil {
        log.Println("No .env file found, loading environment variables")
    }

    // Получение переменных окружения
    dbHost := os.Getenv("POSTGRES_HOST")
    dbPort := os.Getenv("POSTGRES_PORT")
    dbUser := os.Getenv("POSTGRES_USER")
    dbPassword := os.Getenv("POSTGRES_PASSWORD")
    dbName := os.Getenv("POSTGRES_DB")

    // Формирование строки подключения
    dsn := "postgres://" + dbUser + ":" + dbPassword + "@" + dbHost + ":" + dbPort + "/" + dbName + "?sslmode=disable"

    // Подключение к базе данных
    db, err := models.InitDB(dsn)
    if err != nil {
        log.Fatalf("Failed to connect to the database: %v", err)
    }
    defer db.Close()

    // Создание роутера и запуск сервера
    router := mux.NewRouter()
    router.HandleFunc("/api/v0/prices", handlers.PostPrices(db)).Methods("POST")
    router.HandleFunc("/api/v0/prices", handlers.GetPrices(db)).Methods("GET")

    log.Println("Server started on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}
