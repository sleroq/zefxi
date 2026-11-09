package richtext

// Text uses UTF-8 byte offsets for entities, with End exclusive.
type Text struct {
	Value    string
	Entities []Entity
}

type Entity struct {
	Kind   string
	Start  int
	End    int
	URL    string
	UserID string
}

// TelegramEntity carries the UTF-16 offsets used by the Telegram Bot API.
type TelegramEntity struct {
	Kind   string
	Offset int
	Length int
	URL    string
	UserID string
}
