package types

// Intent bitmasks
const (
	IntentGuilds           = 1 << 0
	IntentGuildMembers     = 1 << 1
	IntentPublicGuildMsg   = 1 << 30
	IntentDirectMessage    = 1 << 12
	IntentGroupAndC2C      = 1 << 25
	IntentInteraction      = 1 << 26
)

// DefaultIntents is the standard intent set for full message access.
const DefaultIntents = IntentPublicGuildMsg | IntentDirectMessage | IntentGroupAndC2C | IntentInteraction

// Media file types
const (
	MediaImage = 1
	MediaVideo = 2
	MediaVoice = 3
	MediaFile  = 4
)

// Message types
const (
	MsgTypeText    = 0
	MsgTypeMD      = 2
	MsgTypeInput   = 6
	MsgTypeMedia   = 7
)

// Stream input states
const (
	StreamStateGenerating = 1
	StreamStateDone       = 10
)
