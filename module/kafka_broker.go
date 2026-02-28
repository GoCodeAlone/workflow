package module

import (
	"context"
	"fmt"
	"sync"

	"github.com/CrisisTextLine/modular"
	"github.com/IBM/sarama"
	"github.com/GoCodeAlone/workflow/pkg/tlsutil"
)

// KafkaSASLConfig holds SASL authentication configuration for Kafka.
type KafkaSASLConfig struct {
	Mechanism string `yaml:"mechanism" json:"mechanism"` // PLAIN | SCRAM-SHA-256 | SCRAM-SHA-512
	Username  string `yaml:"username" json:"username"`
	Password  string `yaml:"password" json:"password"` //nolint:gosec // G117: config struct field
}

// KafkaTLSConfig holds TLS and SASL configuration for the Kafka broker.
type KafkaTLSConfig struct {
	tlsutil.TLSConfig `yaml:",inline" json:",inline"`
	SASL              KafkaSASLConfig `yaml:"sasl" json:"sasl"`
}

// KafkaBroker implements the MessageBroker interface using Apache Kafka via Sarama.
type KafkaBroker struct {
	name          string
	brokers       []string
	groupID       string
	producer      sarama.SyncProducer
	consumerGroup sarama.ConsumerGroup
	handlers      map[string]MessageHandler
	mu            sync.RWMutex
	kafkaProducer *kafkaProducerAdapter
	kafkaConsumer *kafkaConsumerAdapter
	cancelFunc    context.CancelFunc
	logger        modular.Logger
	healthy       bool
	healthMsg     string
	encryptor     *FieldEncryptor
	tlsCfg        KafkaTLSConfig
}

// NewKafkaBroker creates a new Kafka message broker.
func NewKafkaBroker(name string) *KafkaBroker {
	broker := &KafkaBroker{
		name:      name,
		brokers:   []string{"localhost:9092"},
		groupID:   "workflow-group",
		handlers:  make(map[string]MessageHandler),
		logger:    &noopLogger{},
		encryptor: NewFieldEncryptorFromEnv(),
	}
	broker.kafkaProducer = &kafkaProducerAdapter{broker: broker}
	broker.kafkaConsumer = &kafkaConsumerAdapter{broker: broker}
	return broker
}

// Name returns the module name.
func (b *KafkaBroker) Name() string {
	return b.name
}

// Init initializes the module with the application context.
func (b *KafkaBroker) Init(app modular.Application) error {
	b.logger = app.Logger()
	return nil
}

// ProvidesServices returns the services provided by this module.
func (b *KafkaBroker) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        b.name,
			Description: "Kafka Message Broker",
			Instance:    b,
		},
		{
			Name:        b.name + ".producer",
			Description: "Kafka Message Producer",
			Instance:    b.kafkaProducer,
		},
		{
			Name:        b.name + ".consumer",
			Description: "Kafka Message Consumer",
			Instance:    b.kafkaConsumer,
		},
	}
}

// RequiresServices returns the services required by this module.
func (b *KafkaBroker) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetBrokers sets the Kafka broker addresses.
func (b *KafkaBroker) SetBrokers(brokers []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.brokers = brokers
}

// SetGroupID sets the Kafka consumer group ID.
func (b *KafkaBroker) SetGroupID(groupID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.groupID = groupID
}

// SetTLSConfig sets the TLS and SASL configuration for the Kafka broker.
func (b *KafkaBroker) SetTLSConfig(cfg KafkaTLSConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tlsCfg = cfg
}

// HealthStatus implements the HealthCheckable interface.
func (b *KafkaBroker) HealthStatus() HealthCheckResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.healthy {
		return HealthCheckResult{Status: "healthy", Message: b.healthMsg}
	}
	return HealthCheckResult{Status: "degraded", Message: b.healthMsg}
}

// setHealthy marks the broker as healthy with an optional message.
func (b *KafkaBroker) setHealthy(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.healthy = true
	b.healthMsg = msg
}

// setUnhealthy marks the broker as unhealthy with a reason.
func (b *KafkaBroker) setUnhealthy(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.healthy = false
	b.healthMsg = msg
}

// Producer returns the message producer interface.
func (b *KafkaBroker) Producer() MessageProducer {
	return b.kafkaProducer
}

// Consumer returns the message consumer interface.
func (b *KafkaBroker) Consumer() MessageConsumer {
	return b.kafkaConsumer
}

// Subscribe is a convenience method to subscribe a handler to a topic.
func (b *KafkaBroker) Subscribe(topic string, handler MessageHandler) error {
	return b.kafkaConsumer.Subscribe(topic, handler)
}

// Start connects to Kafka and begins consuming.
func (b *KafkaBroker) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 3
	config.Producer.Return.Successes = true
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	config.Consumer.Offsets.Initial = sarama.OffsetNewest

	// Apply TLS configuration
	if b.tlsCfg.Enabled {
		tlsCfg, tlsErr := tlsutil.LoadTLSConfig(b.tlsCfg.TLSConfig)
		if tlsErr != nil {
			return fmt.Errorf("kafka broker %q: TLS config: %w", b.name, tlsErr)
		}
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsCfg
	}

	// Apply SASL configuration
	sasl := b.tlsCfg.SASL
	if sasl.Username != "" {
		config.Net.SASL.Enable = true
		config.Net.SASL.User = sasl.Username
		config.Net.SASL.Password = sasl.Password
		switch sasl.Mechanism {
		case "SCRAM-SHA-256":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: SHA256}
			}
		case "SCRAM-SHA-512":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: SHA512}
			}
		default: // PLAIN
			config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		}
	}

	// Create sync producer
	producer, err := sarama.NewSyncProducer(b.brokers, config)
	if err != nil {
		b.healthy = false
		b.healthMsg = fmt.Sprintf("producer connect failed: %v", err)
		return fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	b.producer = producer

	// Create consumer group and start consuming if there are handlers
	if len(b.handlers) > 0 {
		topics := make([]string, 0, len(b.handlers))
		for topic := range b.handlers {
			topics = append(topics, topic)
		}

		group, groupErr := sarama.NewConsumerGroup(b.brokers, b.groupID, config)
		if groupErr != nil {
			_ = producer.Close()
			b.healthy = false
			b.healthMsg = fmt.Sprintf("consumer group connect failed: %v", groupErr)
			return fmt.Errorf("failed to create Kafka consumer group: %w", groupErr)
		}
		b.consumerGroup = group

		consumerCtx, cancel := context.WithCancel(ctx)
		b.cancelFunc = cancel

		handler := &kafkaGroupHandler{
			broker: b,
		}

		go func() {
			for {
				if consumeErr := group.Consume(consumerCtx, topics, handler); consumeErr != nil {
					b.logger.Error("Kafka consumer group error", "error", consumeErr)
					b.setUnhealthy(fmt.Sprintf("consumer error: %v", consumeErr))
				} else {
					b.setHealthy("connected")
				}
				if consumerCtx.Err() != nil {
					return
				}
			}
		}()
	}

	b.healthy = true
	b.healthMsg = "connected"
	b.logger.Info("Kafka broker started", "brokers", b.brokers, "groupID", b.groupID)
	return nil
}

// Stop disconnects from Kafka.
func (b *KafkaBroker) Stop(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cancelFunc != nil {
		b.cancelFunc()
		b.cancelFunc = nil
	}

	var lastErr error

	if b.consumerGroup != nil {
		if err := b.consumerGroup.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close consumer group: %w", err)
			b.logger.Error("Failed to close Kafka consumer group", "error", err)
		}
		b.consumerGroup = nil
	}

	if b.producer != nil {
		if err := b.producer.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close producer: %w", err)
			b.logger.Error("Failed to close Kafka producer", "error", err)
		}
		b.producer = nil
	}

	b.healthy = false
	b.healthMsg = "stopped"
	b.logger.Info("Kafka broker stopped")
	return lastErr
}

// kafkaProducerAdapter implements MessageProducer for Kafka.
type kafkaProducerAdapter struct {
	broker *KafkaBroker
}

// SendMessage publishes a message to a Kafka topic. When ENCRYPTION_KEY is set,
// the message payload is encrypted before publishing to protect PII in transit.
func (p *kafkaProducerAdapter) SendMessage(topic string, message []byte) error {
	p.broker.mu.RLock()
	producer := p.broker.producer
	encryptor := p.broker.encryptor
	p.broker.mu.RUnlock()

	if producer == nil {
		return fmt.Errorf("kafka producer not initialized; call Start first")
	}

	// Encrypt the message payload if encryption is enabled
	payload := message
	if encryptor != nil && encryptor.Enabled() {
		encrypted, err := encryptor.EncryptJSON(message)
		if err != nil {
			return fmt.Errorf("failed to encrypt kafka message for topic %q: %w", topic, err)
		}
		payload = encrypted
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(payload),
	}

	_, _, err := producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message to topic %q: %w", topic, err)
	}

	p.broker.logger.Info("Message sent to Kafka", "topic", topic)
	return nil
}

// kafkaConsumerAdapter implements MessageConsumer for Kafka.
type kafkaConsumerAdapter struct {
	broker *KafkaBroker
}

// Subscribe registers a handler for a topic.
func (c *kafkaConsumerAdapter) Subscribe(topic string, handler MessageHandler) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	c.broker.handlers[topic] = handler
	c.broker.logger.Info("Handler registered for Kafka topic", "topic", topic)
	return nil
}

// Unsubscribe removes the handler for a topic.
func (c *kafkaConsumerAdapter) Unsubscribe(topic string) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	delete(c.broker.handlers, topic)
	c.broker.logger.Info("Handler unregistered from Kafka topic", "topic", topic)
	return nil
}

// kafkaGroupHandler implements sarama.ConsumerGroupHandler.
type kafkaGroupHandler struct {
	broker *KafkaBroker
}

func (h *kafkaGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	h.broker.setHealthy("consuming")
	for msg := range claim.Messages() {
		h.broker.mu.RLock()
		handler, ok := h.broker.handlers[msg.Topic]
		encryptor := h.broker.encryptor
		h.broker.mu.RUnlock()

		if ok {
			// Decrypt message payload if encryption is enabled
			payload := msg.Value
			if encryptor != nil && encryptor.Enabled() {
				decrypted, err := encryptor.DecryptJSON(payload)
				if err != nil {
					h.broker.logger.Error("Error decrypting Kafka message", "topic", msg.Topic, "error", err)
					session.MarkMessage(msg, "")
					continue
				}
				payload = decrypted
			}

			if err := handler.HandleMessage(payload); err != nil {
				h.broker.logger.Error("Error handling Kafka message", "topic", msg.Topic, "error", err)
			}
		}
		session.MarkMessage(msg, "")
	}
	return nil
}
