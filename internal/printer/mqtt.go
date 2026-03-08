package printer

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTClient struct {
	client       mqtt.Client
	commandTopic string
	serial       string

	mu    sync.RWMutex
	data  map[string]any
	ready chan struct{}
}

func NewMQTTClient(ip, accessCode, serial, username string, port int, timeout time.Duration) (*MQTTClient, error) {
	if username == "" {
		username = "bblp"
	}
	if port == 0 {
		port = 8883
	}

	mc := &MQTTClient{
		commandTopic: fmt.Sprintf("device/%s/request", serial),
		serial:       serial,
		data:         map[string]any{},
		ready:        make(chan struct{}),
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tls://%s:%d", ip, port))
	opts.SetUsername(username)
	opts.SetPassword(accessCode)
	opts.SetClientID(fmt.Sprintf("bambu-cli-%d", time.Now().UnixNano()))
	opts.SetConnectTimeout(timeout)
	opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
	opts.SetAutoReconnect(true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		topic := fmt.Sprintf("device/%s/report", serial)
		if token := c.Subscribe(topic, 0, mc.onMessage); token.Wait() && token.Error() != nil {
			return
		}
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {})

	mc.client = mqtt.NewClient(opts)
	if token := mc.client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return mc, nil
}

func (m *MQTTClient) Close() {
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect(250)
	}
}

func (m *MQTTClient) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(msg.Payload()))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return
	}

	// 深度处理函数：递归转换 json.Number 并平铺 print 字段
	var process func(map[string]any)
	process = func(data map[string]any) {
		for k, v := range data {
			switch val := v.(type) {
			case json.Number:
				if f, err := val.Float64(); err == nil {
					data[k] = f
				}
			case map[string]any:
				process(val) // 递归处理
			}
		}
	}

	m.mu.Lock()
	// 1. 全局递归处理类型转换
	process(doc)

	// 2. 合并数据到 m.data
	for k, v := range doc {
		if k == "print" {
			if printMap, ok := v.(map[string]any); ok {
				// 将 print 内部内容提升到顶层，并保留别名
				for pk, pv := range printMap {
					m.data[pk] = pv
					// 强制别名映射，确保万无一失
					if pk == "nozzle_temper" { m.data["nozzle_temp"] = pv }
					if pk == "bed_temper" { m.data["bed_temp"] = pv }
					if pk == "chamber_temper" { m.data["chamber_temp"] = pv }
				}
			}
		}
		m.data[k] = v
	}
	m.mu.Unlock()

	select {
	case <-m.ready:
	default:
		close(m.ready)
	}
}

func (m *MQTTClient) WaitForData(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-m.ready:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout waiting for printer data")
	}
}

func (m *MQTTClient) PushAll() error {
	return m.Publish(map[string]any{"pushing": map[string]any{"command": "pushall"}})
}

func (m *MQTTClient) Publish(payload any) error {
	if m.client == nil || !m.client.IsConnected() {
		return errors.New("mqtt not connected")
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ok := m.client.Publish(m.commandTopic, 0, false, b)
	if !ok.WaitTimeout(5 * time.Second) {
		return errors.New("mqtt publish timeout")
	}
	return ok.Error()
}

func (m *MQTTClient) Snapshot() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]any{}
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

func (m *MQTTClient) Get(path ...string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var current any = m.data
	for _, p := range path {
		mMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mMap[p]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
