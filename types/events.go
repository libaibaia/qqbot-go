package types

// C2CMessageEvent is a C2C (private chat) message.
type C2CMessageEvent struct {
	Author struct {
		ID           string `json:"id"`
		UnionOpenID  string `json:"union_openid"`
		UserOpenID   string `json:"user_openid"`
	} `json:"author"`
	Content      string               `json:"content"`
	ID           string               `json:"id"`
	Timestamp    string               `json:"timestamp"`
	Attachments  []MessageAttachment  `json:"attachments,omitempty"`
	MessageType  int                  `json:"message_type,omitempty"`
}

// GroupMessageEvent is a group chat message.
type GroupMessageEvent struct {
	Author struct {
		ID          string `json:"id"`
		MemberOpenID string `json:"member_openid"`
		Username    string `json:"username,omitempty"`
		Bot         bool   `json:"bot,omitempty"`
	} `json:"author"`
	Content      string               `json:"content"`
	ID           string               `json:"id"`
	Timestamp    string               `json:"timestamp"`
	GroupID      string               `json:"group_id"`
	GroupOpenID  string               `json:"group_openid"`
	Attachments  []MessageAttachment  `json:"attachments,omitempty"`
	Mentions     []Mention            `json:"mentions,omitempty"`
	MessageType  int                  `json:"message_type,omitempty"`
}

// GuildMessageEvent is a guild/channel message.
type GuildMessageEvent struct {
	ID          string `json:"id"`
	ChannelID   string `json:"channel_id"`
	GuildID     string `json:"guild_id"`
	Content     string `json:"content"`
	Timestamp   string `json:"timestamp"`
	Author      struct {
		ID       string `json:"id"`
		Username string `json:"username,omitempty"`
		Bot      bool   `json:"bot,omitempty"`
	} `json:"author"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

// MessageAttachment is an attachment on a message.
type MessageAttachment struct {
	ContentType string `json:"content_type"`
	Filename    string `json:"filename,omitempty"`
	Height      int    `json:"height,omitempty"`
	Width       int    `json:"width,omitempty"`
	Size        int    `json:"size,omitempty"`
	URL         string `json:"url"`
	VoiceWavURL string `json:"voice_wav_url,omitempty"`
	ASRText     string `json:"asr_refer_text,omitempty"`
}

// Mention is a user mention in a group message.
type Mention struct {
	Scope       string `json:"scope,omitempty"`
	ID          string `json:"id,omitempty"`
	UserOpenID  string `json:"user_openid,omitempty"`
	MemberOpenID string `json:"member_openid,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	Bot         bool   `json:"bot,omitempty"`
	IsYou       bool   `json:"is_you,omitempty"`
}

// InteractionEvent is a button interaction callback.
type InteractionEvent struct {
	ID        string `json:"id"`
	Type      int    `json:"type"`
	Scene     string `json:"scene,omitempty"`
	ChatType  int    `json:"chat_type,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	GuildID   string `json:"guild_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	UserOpenID string `json:"user_openid,omitempty"`
	GroupOpenID string `json:"group_openid,omitempty"`
	GroupMemberOpenID string `json:"group_member_openid,omitempty"`
	Version   int    `json:"version"`
	Data      struct {
		Type     int `json:"type"`
		Resolved struct {
			ButtonData string `json:"button_data,omitempty"`
			ButtonID   string `json:"button_id,omitempty"`
			UserID     string `json:"user_id,omitempty"`
			MessageID  string `json:"message_id,omitempty"`
		} `json:"resolved"`
	} `json:"data"`
}

// GroupChangeEvent is a bot added/removed from a group.
type GroupChangeEvent struct {
	Timestamp      string `json:"timestamp"`
	GroupOpenID    string `json:"group_openid"`
	OpMemberOpenID string `json:"op_member_openid"`
}
