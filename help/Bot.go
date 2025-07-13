package help

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var userThresholds = sync.Map{} // map[int64]float64

func SetThreshold(chatID int64, value float64) {
	userThresholds.Store(chatID, value)
}

func GetThreshold(chatID int64) float64 {
	if val, ok := userThresholds.Load(chatID); ok {
		return val.(float64)
	}
	return 5.0 // Значение по умолчанию
}

type Bot interface {
	SendMessage(chatID int64, text string)
}

type TelegramBot struct {
	API *tgbotapi.BotAPI
}

func NewTelegramBot(token string) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания бота: %w", err)
	}
	return &TelegramBot{API: bot}, nil
}

func (tb *TelegramBot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML" // Опционально: для форматирования

	// Добавление клавиатуры (пример)
	// msg.ReplyMarkup = kb.StopKeyboard()

	if _, err := tb.API.Send(msg); err != nil {
		log.Printf("Ошибка отправки сообщения: %v", err)
	}
}

func (tb *TelegramBot) processUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text

	switch {
	case text == "/start":
		tb.SendMessage(chatID, "Добро пожаловать!")

	case text == "/stop":
		StopMonitoring(chatID, tb)

	case text == "/help":
		tb.SendMessage(chatID, "Команды:\n/start — приветствие;\n/setpercent 1.0 — установить порог в процентах;\n/thr — показать текущий порог;\n/run — запустить мониторинг;\n/stop — остановить мониторинг;")

	case text == "/thr":
		val := GetThreshold(chatID)
		tb.SendMessage(chatID, fmt.Sprintf("Текущая ставка: %.2f%%", val))

	case strings.HasPrefix(text, "/setpercent "):
		valStr := strings.TrimPrefix(text, "/setpercent ")
		threshold, err := strconv.ParseFloat(valStr, 64)
		if err != nil || threshold <= 0 || threshold > 100 {
			tb.SendMessage(chatID, "Введите корректное значение: /setpercent 1.5")
		} else {
			SetThreshold(chatID, threshold)
			tb.SendMessage(chatID, fmt.Sprintf("Порог установлен: %.2f%%", threshold))
		}

	case text == "/run":
		if _, ok := userThresholds.Load(chatID); !ok {
			tb.SendMessage(chatID, "Сначала укажите порог: /setpercent 1.5")
			return
		}

		if err := RegisterUser(chatID); err != nil {
			tb.SendMessage(chatID, "Ошибка регистрации пользователя.")
			return
		}

		tb.SendMessage(chatID, "Мониторинг запущен!")

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] goroutine /run: %v", r)
					tb.SendMessage(chatID, "❌ Внутренняя ошибка мониторинга.")
				}
			}()

			StartMonitoring("15m", chatID, tb, "linear", "all", DbGlobal)
		}()
	}
}

func (tb *TelegramBot) StartListening() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := tb.API.GetUpdatesChan(u)

	for update := range updates {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] В процессе обработки update: %v", r)
				}
			}()

			tb.processUpdate(update)
		}()
	}
}
