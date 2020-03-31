package main

import "container/list"

type Notifier struct {
	st chan notifierState
}

type notifierState struct {
	wait *list.List // of chan<- interface{}
}

func NewNotifier() *Notifier {
	n := &Notifier{st: make(chan notifierState, 1)}
	n.st <- notifierState{wait: list.New()}
	return n
}

func (n *Notifier) Register() <-chan interface{} {
	c := make(chan interface{}, 1)
	st := <-n.st
	st.wait.PushBack(c)
	n.st <- st
	return c
}

func (n *Notifier) Unregister(c <-chan interface{}) {
	st := <-n.st
	for e := st.wait.Front(); e != nil; e = e.Next() {
		if e.Value == c {
			st.wait.Remove(e)
			break
		}
	}
	n.st <- st
}

func (n *Notifier) Notify(event interface{}) {
	st := <-n.st
	for e := st.wait.Front(); e != nil; e = e.Next() {
		c := e.Value.(chan interface{})
		c <- event
	}
	n.st <- st
}
