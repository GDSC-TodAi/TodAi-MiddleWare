package queue

// Topology keeps queue names configurable because the final Python worker
// contract may still change.
type Topology struct {
	EmotionQueue string
	STTQueue     string
	ReplyQueue   string
}

func NewTopology(emotionQueue, sttQueue, replyQueue string) Topology {
	return Topology{
		EmotionQueue: emotionQueue,
		STTQueue:     sttQueue,
		ReplyQueue:   replyQueue,
	}
}

func (t Topology) Queues() []string {
	return []string{t.EmotionQueue, t.STTQueue, t.ReplyQueue}
}
