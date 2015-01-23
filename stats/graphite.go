package main

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/litl/galaxy/log"
)

// A single stat line to send to Graphite
type Stat struct {
	Path  string
	Value float64
	TS    time.Time
}

// Format this to Graphite's line-protocol
func (s Stat) String() string {
	return fmt.Sprintf("%s %.2f %d", s.Path, s.Value, s.TS.Unix())
}

type StatSlice []Stat

func (p StatSlice) Len() int           { return len(p) }
func (p StatSlice) Less(i, j int) bool { return p[i].TS.Before(p[j].TS) }
func (p StatSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Connection to a carbon line receiver
type Carbon struct {
	Addr *net.TCPAddr
	conn net.Conn
}

func NewCarbon(addr string) (*Carbon, error) {
	cAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	return &Carbon{Addr: cAddr}, nil
}

func (c *Carbon) reconnect() {
	if c.conn != nil {
		c.conn.Close()
	}

	var err error
	for {
		c.conn, err = net.DialTCP("tcp", nil, c.Addr)
		if err == nil {
			return
		}
		log.Printf("carbon: %s", err)
		time.Sleep(5 * time.Second)
	}
}

// Send a series of Stats to the Carbon daemon.
func (c *Carbon) Collector(statChan chan []Stat) error {
	if c.conn == nil {
		c.reconnect()
	}

	for stats := range statChan {
		if len(stats) == 0 {
			continue
		}
		sort.Sort(StatSlice(stats))

		for _, s := range stats {
		RETRY:
			_, err := fmt.Fprintf(c.conn, "%s %.8f %d\n", s.Path, s.Value, s.TS.Unix())
			if err != nil {
				log.Printf("carbon: %s", err)
				time.Sleep(5 * time.Second)
				c.reconnect()
				goto RETRY
			}
		}
	}

	return nil
}
