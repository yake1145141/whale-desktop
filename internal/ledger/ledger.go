package ledger

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"whale-desktop/internal/config"
	"whale-desktop/internal/jsonc"
)

// Ledger stores the balance-difference usage account.
type Ledger struct {
	Date         string             `json:"date"`
	LastBalance  *float64           `json:"lastBalance"`
	LastCurrency string             `json:"lastCurrency"`
	TodayUsage   float64            `json:"todayUsage"`
	History      map[string]float64 `json:"history"`
}

func TodayKey() string {
	return time.Now().Format("2006-01-02")
}

func Read(paths config.Paths) Ledger {
	data, err := os.ReadFile(paths.Usage)
	if err == nil {
		var result Ledger
		if json.Unmarshal(data, &result) == nil && result.Date != "" {
			if result.History == nil {
				result.History = map[string]float64{}
			}
			return result
		}
	}
	return Ledger{Date: TodayKey(), History: map[string]float64{}}
}

func Write(paths config.Paths, value Ledger) bool {
	return jsonc.WriteAtomic(paths.Usage, value) == nil
}

// Record adds a balance drop to today's usage, rolls over at local midnight,
// and retains the previous 30 days.
func Record(paths config.Paths, balance float64, currency string) Ledger {
	current := TodayKey()
	value := Read(paths)
	if value.History == nil {
		value.History = map[string]float64{}
	}
	currencyChanged := value.LastCurrency != "" && currency != "" && value.LastCurrency != currency
	if value.Date != current {
		if value.Date != "" {
			value.History[value.Date] = value.TodayUsage
		}
		value.Date = current
		value.LastBalance = floatPtr(balance)
		value.LastCurrency = currency
		value.TodayUsage = 0
	} else if currencyChanged {
		value.LastBalance = floatPtr(balance)
		value.LastCurrency = currency
	} else {
		previous := balance
		if value.LastBalance != nil {
			previous = *value.LastBalance
		}
		if balance < previous {
			value.TodayUsage += previous - balance
		}
		value.LastBalance = floatPtr(balance)
		value.LastCurrency = currency
	}
	keys := make([]string, 0, len(value.History))
	for date := range value.History {
		keys = append(keys, date)
	}
	sort.Strings(keys)
	for len(keys) > 30 {
		delete(value.History, keys[0])
		keys = keys[1:]
	}
	Write(paths, value)
	return value
}

func floatPtr(value float64) *float64 {
	return &value
}
