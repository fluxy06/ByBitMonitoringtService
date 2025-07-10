package main

import (
	"log"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"SctiptByBit/bybitobject"
)

// Функция для получения chat ID
func getChatID(bot *bybitobject.TelegramBot) int64 {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := bot.api.GetUpdatesChan(u)
	log.Println("Send any message to the bot to get your chat ID...")

	for update := range updates {
		if update.Message != nil {
			log.Printf("Your chat ID: %d", update.Message.Chat.ID)
			return update.Message.Chat.ID
		}
	}
	return 0
}

func main() {
	// Загрузка переменных окружения
	err := godotenv.Load("case.env")
	if err != nil {
		log.Fatal("Error loading .env file: ", err)
	}

	// Получение токена бота
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}

	// Получение DSN для базы данных
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		log.Fatal("DB_DSN is not set")
	}

	// Подключение к базе данных
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	// Проверка подключения к БД
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("Failed to get generic database object: ", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatal("Failed to ping database: ", err)
	}
	log.Println("Successfully connected to database!")

	// Создание экземпляра бота
	telegramBot, err := bybitobject.NewTelegramBot(token)
	if err != nil {
		log.Fatalf("Error creating bot: %v", err)
	}

	// Получение chat ID
	chatID := getChatID(telegramBot)
	log.Printf("Using chat ID: %d", chatID)

	// Запуск мониторинга
	bybitobject.StartMonitoring(
		5.0,         // threshold
		"15m",       // timeframe
		chatID,      // ваш chat ID
		telegramBot, // бот
		"linear",    // category
		"all",       // mode
		db,          // подключение к БД
	)

	log.Println("Monitoring started!")

	// Бесконечный цикл чтобы программа не завершалась
	for {
		time.Sleep(1 * time.Hour)
	}
}
