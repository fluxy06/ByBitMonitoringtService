package help

// Делаем структуры публичными
type StateKey struct {
	ChatID    int64
	Ticker    string
	Direction string
}

type StateValue struct {
	LastTriggerTime int64
}
