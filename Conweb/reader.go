package ConWeb

import (
	"log"
	"net/http"
)

func WsHandler(w http.ResponseWriter, r *http.Request) {
	conn, _ := upgrader.Upgrade(w, r, nil)
	defer conn.Close() // В конце функции закрываем соединение !
	for {              // Читаем сообщение от клиента
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Println("Ошибка чтения: ", err)
			return
		}

		log.Printf("Получено: %s", p)

		if err := conn.WriteMessage(messageType, []byte("Привет, клиент!")); err != nil {
			log.Println("Ошибка записи: ", err)
			return
		}
	}
}
