package help

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

var DbGlobal *gorm.DB

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
		Start     int64   `json:"start"`
		End       int64   `json:"end"`
		Interval  string  `json:"interval"`
		Open      float64 `json:"open,string"`
		Close     float64 `json:"close,string"`
		High      float64 `json:"high,string"`
		Low       float64 `json:"low,string"`
		Volume    float64 `json:"volume,string"`
		Turnover  float64 `json:"turnover,string"`
		Confirm   bool    `json:"confirm"`
		Timestamp int64   `json:"timestamp"`
	} `json:"data"`
}

var (
	tickerStates   = sync.Map{} // map[stateKey]*stateValue
	userTasksPrice = sync.Map{} // map[int64][]context.CancelFunc
)

const (
	chunkSize        = 5
	cooldownPeriod   = 1800 * time.Second
	bybitWSLinearURL = "wss://stream.bybit.com/v5/public/linear"
	bybitWSSpotURL   = "wss://stream.bybit.com/v5/public/spot"
)

var httpClient = resty.New()

func timeframeToNumber(tf string) string {
	if len(tf) > 0 {
		return string(tf[0])
	}
	return "1"
}

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

func runWSMonitor(
	ctx context.Context,
	tickers []string,
	timeframe string,
	threshold float64,
	chatID int64,
	bot Bot,
	category string,
) {
	log.Printf("[runWSMonitor] chatID=%d, tickers=%v", chatID, tickers)

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC][%d] runWSMonitor crashed: %v", chatID, r)
			bot.SendMessage(chatID, "⚠️ Мониторинг аварийно завершён. Перезапустите /run после устранения.")
		}
	}()

	tf := timeframeToNumber(timeframe)
	url := bybitWSLinearURL
	if category == "spot" {
		url = bybitWSSpotURL
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		log.Printf("[WS][%d] Dial error: %v", chatID, err)
		bot.SendMessage(chatID, "❌ Ошибка подключения к WebSocket Bybit")
		return
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "shutdown")
		log.Printf("[WS][%d] WebSocket закрыт корректно", chatID)
	}()

	// Формируем подписку
	var args []string
	for _, symbol := range tickers {
		topic := fmt.Sprintf("kline.%s.%s", tf, symbol)
		args = append(args, topic)
		log.Printf("[WS][%d] Подписка на: %s", chatID, topic)
	}

	subRequest := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}
	msg, err := json.Marshal(subRequest)
	if err != nil {
		log.Printf("[WS][%d] Ошибка сериализации подписки: %v", chatID, err)
		return
	}

	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		log.Printf("[WS][%d] Ошибка отправки подписки: %v", chatID, err)
		return
	}

	log.Printf("[WS][%d] Подписка успешно отправлена", chatID)

	// Чтение потока WebSocket
	for {
		select {
		case <-ctx.Done():
			log.Printf("[WS][%d] Завершение по ctx.Done()", chatID)
			return
		default:
			readCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			_, data, err := conn.Read(readCtx)
			cancel()

			if err != nil {
				log.Printf("[WS][%d] Read error: %v", chatID, err)
				return
			}

			var kline KlineData
			err = sonic.Unmarshal(data, &kline)
			if err != nil {
				log.Printf("[WS][%d] Ошибка парсинга JSON: %v\nRaw: %s", chatID, err, string(data))
				continue
			}

			if len(kline.Data) == 0 {
				continue
			}

			topicParts := strings.Split(kline.Topic, ".")
			if len(topicParts) < 3 {
				log.Printf("[WS][%d] Неверный топик: %s", chatID, kline.Topic)
				continue
			}

			ticker := topicParts[2]
			entry := kline.Data[0]

			open := entry.Open
			close := entry.Close
			change := (close/open - 1) * 100
			timestamp := entry.Timestamp

			keyLong := StateKey{chatID, ticker, "long"}
			keyShort := StateKey{chatID, ticker, "short"}

			if change >= threshold && checkCooldown(&keyLong, timestamp) {
				msg := fmt.Sprintf("Тикер: %s\nИзменение цены: %.2f%%\nЦена: %.2f$\nТаймфрейм: %s\n🟢 Лонг", ticker, change, close, timeframe)
				bot.SendMessage(chatID, msg)
				updateState(&keyLong, timestamp)
			}

			if change <= -threshold && checkCooldown(&keyShort, timestamp) {
				msg := fmt.Sprintf("Тикер: %s\nИзменение цены: %.2f%%\nЦена: %.2f$\nТаймфрейм: %s\n🔴 Шорт", ticker, change, close, timeframe)
				bot.SendMessage(chatID, msg)
				updateState(&keyShort, timestamp)
			}
		}
	}
}

func checkCooldown(key *StateKey, currentTime int64) bool {
	val, ok := tickerStates.Load(*key)
	if !ok {
		return true
	}
	lastTime := val.(*StateValue).LastTriggerTime
	return currentTime-lastTime >= int64(cooldownPeriod.Seconds())*1000
}

func updateState(key *StateKey, timestamp int64) {
	tickerStates.Store(*key, &StateValue{LastTriggerTime: timestamp})
}

func StartMonitoring(timeframe string, chatID int64, bot Bot, category string, mode string, db *gorm.DB) {
	log.Printf("[StartMonitoring] chatID=%d", chatID)
	threshold := GetThreshold(chatID)
	var tickers []string
	var err error
	if mode == "all" {
		tickers, err = getAllUSDPairs(category)
	} else {
		tickers, err = GetUserTickers(chatID, db)
	}
	if err != nil || len(tickers) == 0 {
		msg := "Не удалось получить тикеры"
		if mode != "all" {
			msg = "У вас нет избранных тикеров"
		}
		bot.SendMessage(chatID, msg)
		return
	}

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

func StopMonitoring(chatID int64, bot Bot) {
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

func GetUserTickers(chatID int64, db *gorm.DB) ([]string, error) {
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

func RegisterUser(chatID int64) error {
	var existing User
	err := DbGlobal.Where("telegram_id = ?", chatID).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	newUser := User{
		TelegramID: chatID,
		CreatedAt:  time.Now(),
	}
	return DbGlobal.Create(&newUser).Error
}
