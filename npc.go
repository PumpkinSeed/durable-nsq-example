package npc

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PumpkinSeed/npc/lib/common"
	"github.com/PumpkinSeed/npc/lib/consumer"
	"github.com/PumpkinSeed/npc/lib/producer"
	"github.com/PumpkinSeed/npc/lib/rpc"
)

// T the type of the npc
type T int

const (
	// Server type
	Server T = iota

	// Client type
	Client
)

// Main handler
type Main struct {
	server bool
	client bool

	// Common data for both handler
	p        *producer.Config
	c        *consumer.Config
	reqTopic string
	logger   common.Logger
	err      error
	channel  string

	// Client related data
	rspTopic  string
	rpcClient *rpc.Client

	// Server releated data
	app               rpc.AppServer
	rpcServer         *rpc.Server
	interruptor       func()
	customInterruptor bool
}

// New creates a new instance of the Main handler based on the type
func New(t T) *Main {
	m := new(Main)
	switch t {
	case Server:
		m.server = true
	case Client:
		m.client = true
	}

	return m
}

func (m *Main) Init(p *producer.Config, c *consumer.Config, rt string, channel string, logger common.Logger) *Main {
	m.p = p
	m.c = c
	m.reqTopic = rt
	m.logger = logger
	m.channel = channel

	if m.p == nil {
		m.err = errors.New("empty producer config")
		return m
	}
	if m.c == nil {
		m.err = errors.New("empty consumer config")
		return m
	}

	return m
}

/*
	Server related methods
*/

func (m *Main) Server(app rpc.AppServer) (*Main, error) {
	m.app = app

	return m, m.err
}

func (m *Main) Listen() error {
	var err error
	// @todo dont listen if it is Client

	p, err := producer.New(m.p)

	// rpc server: accepts request, calls application, sends response
	ctx, cancel := context.WithCancel(context.Background())
	m.rpcServer = rpc.NewServer(ctx, m.app, p)

	c, err := consumer.New(m.c, m.reqTopic, m.channel, m.rpcServer)
	if err != nil {
		return err
	}

	// clean exit
	defer p.Stop() // 3. stop response producer
	defer cancel() // 2. cancel any pending operation (returns unfinished messages to nsq)
	defer c.Stop() // 1. stop accepting new requestser.Stop() // 1. stop accepting new requests

	if m.customInterruptor {
		m.interruptor()
	} else {
		DefaultInterupt()
	}

	return nil
}

func (m *Main) SetInterruptor(i func()) {
	m.customInterruptor = true
	m.interruptor = i
}

/*
	Client related methods
*/

func (m *Main) Client(rt string) (*Main, error) {
	m.rspTopic = rt

	return m, m.err
}

func (m *Main) Publish(typ string, msg []byte) ([]byte, error) {
	var err error
	// @todo dont publish if it is Server

	p, err := producer.New(m.p)

	// rpc client: sends requests, waits and accepts responses
	//             provides interface for application
	rpcClient := rpc.NewClient(p, m.reqTopic, m.rspTopic)

	c, err := consumer.New(m.c, m.rspTopic, m.channel, rpcClient)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	// clean exit
	defer p.Stop() // 3. stop producing new requests
	defer cancel() // 2. cancel any pending (waiting for responses)
	defer c.Stop() // 1. stop listening for responses

	rspBody, rspErr, err := rpcClient.Call(ctx, typ, msg)
	if err != nil {
		return nil, err
	}
	if rspErr != "" {
		return nil, errors.New(rspErr)
	}

	return rspBody, nil
}

func DefaultInterupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
}
