package bybitobject

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot interface {
	SendMessage(chatID int64, text string)
}

type TelegramBot struct {
	api *tgbotapi.BotAPI
}

func NewTelegramBot(token string) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания бота: %w", err)
	}
	return &TelegramBot{api: bot}, nil
}

func (tb *TelegramBot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML" // Опционально: для форматирования

	// Добавление клавиатуры (пример)
	// msg.ReplyMarkup = kb.StopKeyboard()

	if _, err := tb.api.Send(msg); err != nil {
		log.Printf("Ошибка отправки сообщения: %v", err)
	}
}

func (tb *TelegramBot) StartListening() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := tb.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if update.Message.Text == "/start" {
			tb.SendMessage(update.Message.Chat.ID, "Добро пожаловать!")
		}

		if update.Message.Text == "/stop" {
			StopMonitoring(update.Message.Chat.ID, tb)
		}
	}
}
