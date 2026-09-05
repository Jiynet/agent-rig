// Package eventbus 提供模块间解耦的事件发布/订阅能力（可选组件）。
package eventbus

// Bus 是事件总线的抽象接口。
type Bus interface {
	Publish(topic string, data interface{})
	Subscribe(topic string, fn func(data interface{})) func()
}
