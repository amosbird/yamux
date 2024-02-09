package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
)

type multiplex struct {
	io.Reader
	io.Writer
}

func NewReadWriteCloser(r io.Reader, w io.Writer) *multiplex {
	return &multiplex{Reader: r, Writer: w}
}

func (m *multiplex) Close() error {
	return nil
}

type multiplex2 struct {
	io.ReadCloser
	io.WriteCloser
}

func NewReadWriteCloser2(r io.ReadCloser, w io.WriteCloser) *multiplex2 {
	return &multiplex2{ReadCloser: r, WriteCloser: w}
}

func (m *multiplex2) Close() error {
	err := m.WriteCloser.Close()
	err2 := m.ReadCloser.Close()
	if err == nil {
		err = err2
	}
	return err
}

var (
	port     int
	ssh      string
	isServer bool
)

func main() {
	flag.IntVar(&port, "port", 20222, "help message")
	flag.StringVar(&ssh, "ssh", "ssh 127.0.0.1 git/yamux/yamux --server --port 8123", "help message")
	flag.BoolVar(&isServer, "server", false, "help message")
	flag.Parse()

	if isServer {
		if err := server(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	} else {
		if err := client(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}

func copyConn(src net.Conn, dst net.Conn, log Logger) {
	go func() {
		written, _ := io.Copy(src, dst)
		log.Printf("wrote %d bytes from dst to src\n", written)
	}()

	go func() {
		written, _ := io.Copy(dst, src)
		log.Printf("wrote %d bytes from src to dst\n", written)
	}()
}

func client() error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	cmd := exec.Command("bash", "-c", ssh)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	conn := NewReadWriteCloser2(stdout, stdin)

	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "An error occured: ", err)
		return err
	}

	defer cmd.Process.Kill()

	log.Println("creating client session")
	config := DefaultConfig()
	config.EnableKeepAlive = false
	config.Logger = log.New(os.Stderr, "client: ", log.LstdFlags)
	config.LogOutput = nil
	session, err := Client(conn, config)
	if err != nil {
		config.Logger.Println("create session err:", err)
		return err
	}
	defer session.Close()

	for {
		src, err := listener.Accept()
		if err != nil {
			log.Println("listen err:", err)
			return err
		}

		dst, err := session.Open()
		if err != nil {
			log.Println("opening stream err:", err)
			return err
		}

		log.Println("start forwarding")
		go copyConn(src, dst, config.Logger)
	}
}

func server() error {
	conn := NewReadWriteCloser(os.Stdin, os.Stdout)
	config := DefaultConfig()
	config.EnableKeepAlive = false
	config.Logger = log.New(os.Stderr, "server: ", log.LstdFlags)
	config.LogOutput = nil
	session, err := Server(conn, config)
	if err != nil {
		return err
	}
	defer session.Close()

	for {
		src, err := session.Accept()
		if err != nil {
			log.Println("session err:", err)
			return err
		}

		dst, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			log.Println("dial err:", err)
			return err
		}

		go copyConn(src, dst, config.Logger)
	}
}
