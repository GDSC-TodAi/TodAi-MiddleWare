package queue

const (
	DefaultRabbitMQURL      = "amqp://guest:guest@localhost:5672/"
	DefaultEmotionQueueName = "todai.worker.emotion"
	DefaultSTTQueueName     = "todai.worker.stt"
)

// Topology keeps queue names configurable because the final Python worker
// contract may still change.
type Topology struct {
	EmotionQueue string
	STTQueue     string
}

func NewTopology(emotionQueue, sttQueue string) Topology {
	if emotionQueue == "" {
		emotionQueue = DefaultEmotionQueueName
	}
	if sttQueue == "" {
		sttQueue = DefaultSTTQueueName
	}

	return Topology{
		EmotionQueue: emotionQueue,
		STTQueue:     sttQueue,
	}
}

func (t Topology) WorkerQueues() []string {
	return []string{t.EmotionQueue, t.STTQueue}
}
