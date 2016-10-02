package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/go-p9p"
)

type ConnectionServerEntry struct {
	name   string
	id     *p9p.Qid
	result string
}

func NewConnectionServerEntry(name string) *ConnectionServerEntry {
	var cse ConnectionServerEntry
	cse.name = name
	cse.id = &p9p.Qid{Type: p9p.QTFILE, Version: 0, Path: ENTRYCOUNT}
	ENTRIES = append(ENTRIES, &cse)
	ENTRYCOUNT++
	return &cse
}

func (cse *ConnectionServerEntry) Qid() p9p.Qid {
	return *cse.id
}

func (cse *ConnectionServerEntry) DirStat() *p9p.Dir {
	return &p9p.Dir{
		Qid:    *cse.id,
		Mode:   STDFILEMODE,
		Length: uint64(0),
		Name:   cse.name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "Nobody",
	}
}

func (cse *ConnectionServerEntry) Size() uint32 {
	return uint32(0)
}

func (cse *ConnectionServerEntry) Read(offset uint64, count uint32) ([]byte, error) {
	return []byte(cse.result), nil
}

func (cse *ConnectionServerEntry) Write(data []byte, offset uint64) (uint32, error) {
	command := string(data)
	components := strings.Split(command, "!")
	result := "/net/"
	if len(components) != 3 {
		return 0, errors.New("Unknown command: " + command)
	}
	proto := components[0]
	if proto != "tcp" && proto != "udp" {
		return 0, errors.New("Uknown protocol: " + proto)
	}
	result += proto + "/clone "

	host := components[1]

	ips, err := net.LookupIP(host)
	if err != nil {
		return 0, err
	}

	service := components[2]
	port := service
	if _, err := strconv.ParseUint(service, 10, 64); err != nil {
		n, err := net.LookupPort(proto, service)
		if err != nil {
			return 0, nil
		}
		port = strconv.Itoa(n)
	}

	results := ""
	for _, ip := range ips {
		// TODO figure out how to pass multiple entries from the DNS through the connection service
		results += result + ip.String() + "!" + port
		break
	}

	cse.result = results

	return 0, nil
}

type NetConn struct {
	mutex sync.Mutex
	proto string
	conn  net.Conn
	err   error
}

func (nc *NetConn) Command(cmd string) error {
	if strings.HasPrefix(cmd, "connect") {
		if nc.conn != nil {
			return errors.New("Connection already active.")
		}
		// TODO better parsing, support for local port dialing
		components := strings.Split(cmd, " ")
		address := components[1]
		address = strings.Replace(address, "!", ":", 1)
		fmt.Printf("Dialing %v\n", address)
		conn, err := net.Dial(nc.proto, address)
		if err != nil {
			return err
		} else {
			nc.conn = conn
		}
		return nil
	} else if strings.HasPrefix(cmd, "hangup") && nc.proto == "tcp" {
		if nc.conn != nil {
			err := nc.conn.Close()
			return err
		}
	}

	return errors.New("Unknown command: " + cmd)
}

func AddNewConn(cf *CloneFileEntry, dir *DirEntry) {
	newDir := NewDirEntry(strconv.Itoa(cf.Number))
	dir.AddChild(newDir)

	newDir.AddChild(cf)

	nc := &NetConn{proto: dir.Name}
	cf.netconn = nc

	data := NewNetConnFile("data", nc)
	newDir.AddChild(data)

	listen := NewNetConnFile("listen", nc)
	newDir.AddChild(listen)

	local := NewNetConnFile("local", nc)
	newDir.AddChild(local)
	remote := NewNetConnFile("remote", nc)
	newDir.AddChild(remote)

	status := NewNetConnFile("status", nc)
	newDir.AddChild(status)

	e := NewNetConnFile("err", nc)
	newDir.AddChild(e)
}

type NetConnFile struct {
	id   *p9p.Qid
	name string
	nc   *NetConn
}

func NewNetConnFile(name string, nc *NetConn) *NetConnFile {
	var ncf NetConnFile
	ncf.name = name
	ncf.nc = nc
	ncf.id = &p9p.Qid{Type: p9p.QTFILE, Version: 0, Path: ENTRYCOUNT}
	ENTRIES = append(ENTRIES, &ncf)
	ENTRYCOUNT++
	return &ncf
}

func (ncf *NetConnFile) Qid() p9p.Qid {
	return *ncf.id
}

func (ncf *NetConnFile) DirStat() *p9p.Dir {
	return &p9p.Dir{
		Qid:    *ncf.id,
		Mode:   STDFILEMODE,
		Length: uint64(0),
		Name:   ncf.name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "Nobody",
	}
}

func (ncf *NetConnFile) Size() uint32 {
	return 0
}

func (ncf *NetConnFile) Read(offset uint64, count uint32) ([]byte, error) {
	// TODO descriminate between the data file, listen, etc.
	if ncf.nc.conn == nil {
		return []byte{}, errors.New("No connection established, cannot read data")
	}
	buf := make([]byte, int(count))
	n, err := ncf.nc.conn.Read(buf)
	if err != nil && n < 0 {
		return []byte{}, err
	}

	fmt.Printf("READ: %v", string(buf[:n]))

	return buf[:n], err
}

func (ncf *NetConnFile) Write(data []byte, offset uint64) (uint32, error) {
	// TODO descriminate between the data file, listen, etc.
	if ncf.nc.conn == nil {
		return 0, errors.New("No connection established, cannot write data")
	}

	fmt.Printf("WRITE: %v", string(data))

	n, err := ncf.nc.conn.Write(data)
	return uint32(n), err
}
