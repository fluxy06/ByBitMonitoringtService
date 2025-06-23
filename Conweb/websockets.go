package ConWeb

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// Настройка Upgrader
var upgrader = websocket.Upgrader{ // Основываясь на структуре создаем объект upgrader
	ReadBufferSize:  1024, // Размер для чтения
	WriteBufferSize: 1024, // Размер для записи
	CheckOrigin: func(r *http.Request) bool { //Функция всегда возвращающая true;
		return true
	},
}
