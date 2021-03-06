package conn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/toms1441/resistance-server/internal/client"
	"github.com/toms1441/resistance-server/internal/logger"
)

type mock struct {
	log  logger.Logger
	cmd  map[string]MessageStruct
	mtx  sync.Mutex
	done []chan bool
	pipe net.Conn
	cl   client.Client
}

func (m *mock) MarshalJSON() ([]byte, error) {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	return json.Marshal(struct{}{})
}

func NewMockConnHelper(cl client.Client) (sconn Conn, cconn Conn) {
	spipe, cpipe := net.Pipe()

	var slog, clog logger.Logger
	if slog == nil {
		slog = logger.NullLogger()
	}

	if clog == nil {
		clog = logger.NullLogger()
	}

	sconn, cconn = NewMockConn(spipe, client.Client{}), NewMockConn(cpipe, cl)
	sconn.SetLogger(slog)
	cconn.SetLogger(clog)

	return sconn, cconn
}

func NewMockConn(cn net.Conn, cl client.Client) Conn {

	m := &mock{
		pipe: cn,
		cl:   cl,
		log:  logger.NullLogger(),
	}

	m.cmd = map[string]MessageStruct{}

	go func(m *mock) {
		size := 1024 * 8
		body := make([]byte, size)
		for {
			n, err := m.pipe.Read(body)
			if n == 0 {
				continue
			}

			if err != nil {
				//fmt.Println("destroyed conn from read")
				m.Destroy()
			}

			bts := bytes.Trim(body, "\x00")

			messagejson := MessageRecv{}
			err = json.Unmarshal(bts, &messagejson)
			if err != nil {
				fmt.Printf("error with json: %s.%s: %v\n", messagejson.Group, messagejson.Name, err)
				fmt.Printf("%s\n", string(bts))
			}

			if len(messagejson.Group) > 0 {
				m.mtx.Lock()
				strct, ok := m.cmd[messagejson.Group]
				if ok {
					if len(messagejson.Name) > 0 {
						callback, ok := strct[messagejson.Name]
						if ok {
							go func() {
								err = callback(m.log, messagejson.Body)
								if err != nil {
									fullname := fmt.Sprintf("%s.%s", messagejson.Group, messagejson.Name)
									m.log.Debug("c.MessageRecv: %s %v", fullname, err)
								}
							}()
						}
					}
				} else {
					m.log.Warn("!c.cmd.(bool): %s.%s", messagejson.Group, messagejson.Name)
				}

				m.mtx.Unlock()
			}

			body = make([]byte, size)
		}
	}(m)

	return m
}

func (m *mock) AddCommand(group string, msgstrct MessageStruct) {
	defer m.mtx.Unlock()
	m.mtx.Lock()
	cmd, ok := m.cmd[group]
	// if group exists
	if ok {
		for k, v := range msgstrct {
			cmd[k] = v
		}

		m.cmd[group] = cmd
	} else {
		m.cmd[group] = msgstrct
	}

}

func (m *mock) ExecuteCommand(group, name string, body []byte) error {
	defer m.mtx.Unlock()
	m.mtx.Lock()
	g, ok := m.cmd[group]
	if ok {
		cmd, ok := g[name]
		if ok {
			val := cmd(m.log, body)
			return val
		}

		return fmt.Errorf("c.cmd[name].(bool) != true")
	}

	return fmt.Errorf("c.cmd[group].(bool) != true")
}

func (m *mock) RemoveCommandsByGroup(group string) {
	m.mtx.Lock()
	delete(m.cmd, group)
	m.mtx.Unlock()
	m.log.Debug("conn.RemoveCommandsByGroup: %s", group)
}

func (m *mock) RemoveCommandsByNames(group string, names ...string) {
	m.mtx.Lock()
	_, ok := m.cmd[group]
	defer m.mtx.Unlock()
	if ok {
		for _, v := range names {
			delete(m.cmd[group], v)
			m.log.Debug("conn.RemoveCommandsByNames: %s.%s", group, v)
		}
	}
}

func (m *mock) WriteMessage(ms MessageSend) error {
	body, err := json.Marshal(ms)
	if err != nil {
		return fmt.Errorf("json.Marshal: %w", err)
	}

	m.WriteBytes(body)
	m.log.Debug("c.SendMessage: %s.%s", ms.Group, ms.Name)

	return nil
}

func (m *mock) WriteBytes(body []byte) {
	m.pipe.SetWriteDeadline(time.Now().Add(time.Millisecond * 50))
	m.pipe.Write(body)
}

func (m *mock) GetDone() chan bool {
	var done = make(chan bool)
	if m.done == nil {
		m.done = []chan bool{}
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.done = append(m.done, done)
	return done
}

func (m *mock) Destroy() {

	defer m.mtx.Unlock()
	m.mtx.Lock()

	for _, v := range m.done {
		v <- true
	}

	m.pipe.Close()
}

func (m *mock) GetClient() (c client.Client) {
	return m.cl
}

func (m *mock) SetLogger(log logger.Logger) {
	m.log = log
}
