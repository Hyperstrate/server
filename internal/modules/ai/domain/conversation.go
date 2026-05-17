package domain

import "time"

// Conversation groups a sequence of messages exchanged with a specific model.
type Conversation struct {
	ID        string    `json:"id"              gorm:"primaryKey;size:50"`
	OrgID     string    `json:"-"               gorm:"size:50;not null;index"`
	ModelID   string    `json:"modelId"         gorm:"size:50;not null;index"`
	Title     string    `json:"title,omitempty" gorm:"size:255"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt" gorm:"autoUpdateTime"`
}

// ConversationMessage is a single turn (user or assistant) within a Conversation.
// Fields stores the full inference fields map (prompt, image, systemPrompt, …) as
// a JSON object so the frontend can replay the exact payload (including images).
type ConversationMessage struct {
	ID             string    `json:"id"               gorm:"primaryKey;size:50"`
	ConversationID string    `json:"conversationId"   gorm:"size:50;not null;index"`
	Role           string    `json:"role"             gorm:"size:20;not null"`
	Content        string    `json:"content"          gorm:"type:text;not null"`
	Fields         string    `json:"fields,omitempty" gorm:"type:text"`
	CreatedAt      time.Time `json:"createdAt"`
}
