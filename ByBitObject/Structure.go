package bybitobject

// Ключи для tickerStates
type stateKey struct {
	chatID    int64
	ticker    string
	direction string
}

type stateValue struct {
	lastTriggerTime int64
}
