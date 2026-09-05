package eventbus

import "sync"

// SimpleBus 是内存态事件总线实现。
type SimpleBus struct {
	mu   sync.RWMutex
	next uint64
	subs map[string]map[uint64]func(data interface{})
}

// NewSimpleBus 创建事件总线。
func NewSimpleBus() *SimpleBus {
	return &SimpleBus{subs: make(map[string]map[uint64]func(data interface{}))}
}

// Publish 向某主题的所有订阅者广播数据。
func (b *SimpleBus) Publish(topic string, data interface{}) {
	b.mu.RLock()
	topicSubs := b.subs[topic]
	listeners := make([]func(data interface{}), 0, len(topicSubs))
	for _, fn := range topicSubs {
		listeners = append(listeners, fn)
	}
	b.mu.RUnlock()

	for _, fn := range listeners {
		fn(data)
	}
}

// Subscribe 订阅某主题，返回取消订阅函数。
func (b *SimpleBus) Subscribe(topic string, fn func(data interface{})) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.next++
	id := b.next
	if b.subs[topic] == nil {
		b.subs[topic] = make(map[uint64]func(data interface{}))
	}
	b.subs[topic][id] = fn

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[topic], id)
	}
}
