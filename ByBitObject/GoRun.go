package bybitobject

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/go-resty/resty/v2"
	"gorm.io/gorm"
	"nhooyr.io/websocket"
)

// Структуры для работы с Bybit API
type InstrumentInfoResponse struct {
	Result struct {
		List []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			BaseCoin   string `json:"baseCoin"`
			QuoteCoin  string `json:"quoteCoin"`
			LaunchTime string `json:"launchTime"`
		} `json:"list"`
	} `json:"result"`
	RetCode int `json:"retCode"`
}

type KlineData struct {
	Topic string `json:"topic"`
	Data  []struct {
		Start     string  `json:"start"`
		Open      float64 `json:"open,string"`
		Close     float64 `json:"close,string"`
		High      float64 `json:"high,string"`
		Low       float64 `json:"low,string"`
		Timestamp int64   `json:"timestamp"`
	} `json:"data"`
}

// Глобальные переменные с использованием sync.Map для конкурентного доступа
var (
	tickerStates   = sync.Map{} // map[stateKey]*stateValue
	userTasksPrice = sync.Map{} // map[int64][]context.CancelFunc
)

// Конфигурация
const (
	chunkSize        = 5
	cooldownPeriod   = 1800 * time.Second // 30 минут
	bybitWSLinearURL = "wss://stream.bybit.com/v5/public/linear"
	bybitWSSpotURL   = "wss://stream.bybit.com/v5/public/spot"
)

// HTTP клиент для REST запросов
var httpClient = resty.New()

// Преобразование таймфрейма
func timeframeToNumber(tf string) string {
	if len(tf) > 0 {
		return string(tf[0])
	}
	return "1"
}

// Получение USDT пар
func getAllUSDPairs(category string) ([]string, error) {
	resp, err := httpClient.R().
		SetQueryParam("category", category).
		Get("https://api.bybit.com/v5/market/instruments-info")

	if err != nil {
		return nil, err
	}

	var info InstrumentInfoResponse
	if err := sonic.Unmarshal(resp.Body(), &info); err != nil {
		return nil, err
	}

	var pairs []string
	for _, inst := range info.Result.List {
		if strings.HasSuffix(inst.Symbol, "USDT") && inst.Status == "Trading" {
			pairs = append(pairs, inst.Symbol)
		}
	}
	return pairs, nil
}

// Запуск мониторинга WebSocket
func runWSMonitor(
	ctx context.Context,
	tickers []string,
	timeframe string,
	threshold float64,
	chatID int64,
	bot Bot,
	category string,
) {
	tf := timeframeToNumber(timeframe)
	url := bybitWSLinearURL
	if category == "spot" {
		url = bybitWSSpotURL
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		log.Printf("WebSocket connection error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "Internal error")

	// Формирование подписки
	var args []string
	for _, symbol := range tickers {
		args = append(args, fmt.Sprintf("kline.%s.%s", tf, symbol))
	}

	subRequest := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}

	msg, _ := json.Marshal(subRequest)
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		log.Printf("Subscribe error: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, data, err := conn.Read(ctx)
			if err != nil {
				log.Printf("Read error: %v", err)
				return
			}

			var kline KlineData
			if err := sonic.Unmarshal(data, &kline); err != nil {
				continue
			}

			if len(kline.Data) == 0 {
				continue
			}

			topicParts := strings.Split(kline.Topic, ".")
			if len(topicParts) < 3 {
				continue
			}
			ticker := topicParts[2]

			open := kline.Data[0].Open
			close := kline.Data[0].Close
			change := (close/open - 1) * 100

			timestamp := kline.Data[0].Timestamp
			keyLong := stateKey{chatID, ticker, "long"}
			keyShort := stateKey{chatID, ticker, "short"}

			// Проверка лонг сигнала
			if change >= threshold {
				if checkCooldown(&keyLong, timestamp) {
					msg := fmt.Sprintf(
						"Тикер: %s\nИзменение цены: %.2f%%\nПоследняя цена: %.2f$\nТаймфрейм: %s\nНаправление: 🟢Лонг",
						ticker, change, close, timeframe,
					)
					bot.SendMessage(chatID, msg)
					updateState(&keyLong, timestamp)
				}
			}

			// Проверка шорт сигнала
			if change <= -threshold {
				if checkCooldown(&keyShort, timestamp) {
					msg := fmt.Sprintf(
						"Тикер: %s\nИзменение цены: %.2f%%\nПоследняя цена: %.2f$\nТаймфрейм: %s\nНаправление: 🔴Шорт",
						ticker, change, close, timeframe,
					)
					bot.SendMessage(chatID, msg)
					updateState(&keyShort, timestamp)
				}
			}
		}
	}
}

// Проверка интервала между уведомлениями
func checkCooldown(key *stateKey, currentTime int64) bool {
	val, ok := tickerStates.Load(*key)
	if !ok {
		return true
	}

	lastTime := val.(*stateValue).lastTriggerTime
	return currentTime-lastTime >= int64(cooldownPeriod.Seconds())*1000
}

// Обновление состояния
func updateState(key *stateKey, timestamp int64) {
	tickerStates.Store(*key, &stateValue{
		lastTriggerTime: timestamp,
	})
}

// Запуск мониторинга
func startMonitoring(
	threshold float64,
	timeframe string,
	chatID int64,
	bot Bot,
	category string,
	mode string,
	db *gorm.DB,
) {
	var tickers []string
	var err error

	if mode == "all" {
		tickers, err = getAllUSDPairs(category)
	} else {
		tickers, err = getUserTickers(chatID, db)
	}

	if err != nil || len(tickers) == 0 {
		msg := "Не удалось получить тикеры"
		if mode != "all" {
			msg = "У вас нет избранных тикеров"
		}
		bot.SendMessage(chatID, msg)
		return
	}

	// Разбивка на чанки
	totalChunks := int(math.Ceil(float64(len(tickers)) / float64(chunkSize)))
	var cancelFuncs []context.CancelFunc

	for i := 0; i < totalChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(tickers) {
			end = len(tickers)
		}
		chunk := tickers[start:end]

		ctx, cancel := context.WithCancel(context.Background())
		go runWSMonitor(ctx, chunk, timeframe, threshold, chatID, bot, category)

		cancelFuncs = append(cancelFuncs, cancel)
	}

	userTasksPrice.Store(chatID, cancelFuncs)
}

// Остановка мониторинга
func stopMonitoring(chatID int64, bot Bot) {
	if val, ok := userTasksPrice.Load(chatID); ok {
		cancelFuncs := val.([]context.CancelFunc)
		for _, cancel := range cancelFuncs {
			cancel()
		}
		userTasksPrice.Delete(chatID)
		bot.SendMessage(chatID, "Мониторинг остановлен")
	} else {
		bot.SendMessage(chatID, "Нет активного мониторинга")
	}
}

// Получение тикеров пользователя из БД
func getUserTickers(chatID int64, db *gorm.DB) ([]string, error) {
	var userTickers []struct {
		NameTicker string
	}

	err := db.Table("user_tickers").
		Joins("JOIN users ON users.id = user_tickers.user_id").
		Where("users.telegram_id = ?", chatID).
		Pluck("name_ticker", &userTickers).
		Error

	if err != nil {
		return nil, err
	}

	tickers := make([]string, len(userTickers))
	for i, t := range userTickers {
		tickers[i] = t.NameTicker
	}
	return tickers, nil
}

// Интерфейс бота для отправки сообщений
type Bot interface {
	SendMessage(chatID int64, text string)
}
