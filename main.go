package main

import (
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ScriptByBit/help"
)

func main() {
	// Загрузка .env
	if err := godotenv.Load("case.env"); err != nil {
		log.Fatal("Ошибка загрузки .env:", err)
	}

	// Получение токена
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не установлен")
	}

	// Подключение к базе данных
	dsn := os.Getenv("DB_DSN")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		log.Fatal("Ошибка ping к БД:", err)
	}
	log.Println("База данных подключена")

	// Создание бота
	telegramBot, err := help.NewTelegramBot(token)
	if err != nil {
		log.Fatal("Ошибка создания Telegram-бота:", err)
	}

	// 🛠 Удаляем старый webhook, если он есть
	if _, err := telegramBot.API.Request(tgbotapi.DeleteWebhookConfig{
		DropPendingUpdates: true,
	}); err != nil {
		log.Fatalf("Ошибка удаления webhook: %v", err)
	}

	// Устанавливаем БД глобально
	help.DbGlobal = db

	// Запуск слушателя бота
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] в StartListening: %v", r)
			}
		}()
		log.Println("Бот запущен и слушает команды...")
		telegramBot.StartListening()
	}()

	select {} // блокируем main-поток
}
