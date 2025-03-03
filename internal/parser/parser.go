package parser

import (
	"errors"
	"strconv"
	"strings"
)

func ParseStats(line string) ([7]float64, error) {
	var result [7]float64
	parts := strings.Split(strings.TrimSpace(line), ",")
	if len(parts) != 7 {
		return result, errors.New("invalid stats format")
	}

	for i, p := range parts {
		val, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return result, err
		}
		result[i] = val
	}

	return result, nil
}
