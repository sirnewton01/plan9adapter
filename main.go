package main

import (
	"bytes"
	"errors"
	"flag"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/docker/go-p9p"
	"golang.org/x/net/context"
)

var (
	addr        string
	STDFILEMODE uint32
	STDDIRMODE  uint32

	FIDS       map[p9p.Fid]uint64
	ENTRYCOUNT uint64  = 0
	ENTRIES    []Entry = make([]Entry, 0)
)

type DirEntry struct {
	Name     string
	Id       *p9p.Qid
	Children []*p9p.Dir
}

func NewDirEntry(name string) *DirEntry {
	var direntry DirEntry
	direntry.Name = name
	direntry.Id = &p9p.Qid{Type: p9p.QTDIR, Version: 0, Path: ENTRYCOUNT}
	ENTRIES = append(ENTRIES, &direntry)
	ENTRYCOUNT++
	return &direntry
}

func (direntry *DirEntry) AddChild(child Entry) {
	if direntry.Children == nil {
		direntry.Children = make([]*p9p.Dir, 0)
	}
	// No duplicates
	direntry.RemoveChild(child)
	direntry.Children = append(direntry.Children, child.DirStat())
}

func (direntry *DirEntry) RemoveChild(entry Entry) {
	if direntry.Children == nil {
		return
	}

	for idx, child := range direntry.Children {
		if entry.Qid().Path == child.Qid.Path {
			direntry.Children = append(direntry.Children[:idx], direntry.Children[idx+1:]...)
			return
		}
	}
}

func (direntry *DirEntry) FindChild(name string) (Entry, bool) {
	if direntry.Children == nil {
		return direntry, false
	}

	for _, child := range direntry.Children {
		if child.Name == name {
			return ENTRIES[child.Qid.Path], true
		}
	}

	return direntry, false
}

func (direntry *DirEntry) Qid() p9p.Qid {
	return *direntry.Id
}

func (direntry *DirEntry) DirStat() *p9p.Dir {
	return &p9p.Dir{
		Qid:    *direntry.Id,
		Mode:   STDDIRMODE,
		Length: 0,
		Name:   direntry.Name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "nobody",
	}
}

func (direntry *DirEntry) Size() uint32 {
	if direntry.Children == nil {
		return uint32(0)
	}

	// TODO perhaps we should cache the size
	return uint32(len(direntry.Contents()))
}

func (direntry *DirEntry) Contents() []byte {
	var buffer bytes.Buffer

	if direntry.Children != nil {
		codec := p9p.NewCodec()
		for _, dir := range direntry.Children {
			// TODO check for errors
			p9p.EncodeDir(codec, &buffer, dir)
		}
	}

	return buffer.Bytes()
}

func (direntry *DirEntry) Write(data []byte, offset uint64) (uint32, error) {
	return 0, errors.New("Cannot write to directories")
}

type StaticFileEntry struct {
	Name string
	Id   *p9p.Qid
	Data []byte
}

func NewStaticFileEntry(name string, contents string) *StaticFileEntry {
	var staticfile StaticFileEntry
	staticfile.Name = name
	staticfile.Id = &p9p.Qid{Type: p9p.QTFILE, Version: 0, Path: ENTRYCOUNT}
	staticfile.Data = []byte(contents)
	ENTRIES = append(ENTRIES, &staticfile)
	ENTRYCOUNT++
	return &staticfile
}

func (staticfile *StaticFileEntry) Qid() p9p.Qid {
	return *staticfile.Id
}

func (staticfile *StaticFileEntry) DirStat() *p9p.Dir {
	return &p9p.Dir{
		Qid:    *staticfile.Id,
		Mode:   STDFILEMODE,
		Length: uint64(len(staticfile.Data)),
		Name:   staticfile.Name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "nobody",
	}
}

func (staticfile *StaticFileEntry) Size() uint32 {
	return uint32(len(staticfile.Data))
}

func (staticfile *StaticFileEntry) Contents() []byte {
	return staticfile.Data
}

func (staticfile *StaticFileEntry) Write(data []byte, offset uint64) (uint32, error) {
	return 0, errors.New("Cannot write to static file")
}

type CloneFileEntry struct {
	cloned bool
	Number int
	Id     *p9p.Qid
	Proto  *DirEntry
}

func NewCloneFileEntry(proto *DirEntry, number int) *CloneFileEntry {
	var clonefile CloneFileEntry
	clonefile.Id = &p9p.Qid{Type: p9p.QTFILE, Version: 0, Path: ENTRYCOUNT}
	ENTRIES = append(ENTRIES, &clonefile)
	ENTRYCOUNT++
	clonefile.Proto = proto
	clonefile.Number = number
	return &clonefile
}

func (clonefile *CloneFileEntry) Qid() p9p.Qid {
	return *clonefile.Id
}

func (clonefile *CloneFileEntry) DirStat() *p9p.Dir {
	name := "clone"
	if clonefile.cloned {
		name = "ctl"
	}

	return &p9p.Dir{
		Qid:    *clonefile.Id,
		Mode:   STDFILEMODE,
		Length: uint64(clonefile.Size()),
		Name:   name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "Nobody",
	}
}

func (clonefile *CloneFileEntry) Size() uint32 {
	return uint32(len([]byte(strconv.Itoa(clonefile.Number))))
}

func (clonefile *CloneFileEntry) Contents() []byte {
	return []byte(strconv.Itoa(clonefile.Number))
}

func (clonefile *CloneFileEntry) Write(data []byte, offset uint64) (uint32, error) {
	return 0, errors.New("Clone file doesn't support write")
}

func (clonefile *CloneFileEntry) Clone() {
	if clonefile.cloned {
		return
	}
	parent := clonefile.Proto
	parent.RemoveChild(clonefile)
	parent.AddChild(NewCloneFileEntry(parent, clonefile.Number+1))
	clonefile.cloned = true

	newdir := NewDirEntry(strconv.Itoa(clonefile.Number))
	newdir.AddChild(clonefile)
	parent.AddChild(newdir)
}

type Entry interface {
	Qid() p9p.Qid
	DirStat() *p9p.Dir
	Size() uint32
	Contents() []byte
	Write(data []byte, offset uint64) (uint32, error)
}

func init() {
	flag.StringVar(&addr, "addr", ":5640", "bind addr for 9p server, prefix with unix: for unix socket")

	STDDIRMODE = p9p.DMDIR | (p9p.DMREAD|p9p.DMEXEC)<<8 | (p9p.DMREAD|p9p.DMEXEC)<<4 | (p9p.DMREAD | p9p.DMEXEC)
	STDFILEMODE = (p9p.DMREAD|p9p.DMEXEC)<<8 | (p9p.DMREAD|p9p.DMEXEC)<<4 | (p9p.DMREAD | p9p.DMEXEC)
	FIDS = make(map[p9p.Fid]uint64)

	// Set up the basic structure of /net
	root := NewDirEntry("")
	net := NewDirEntry("net")
	root.AddChild(net)

	udp := NewDirEntry("udp")
	net.AddChild(udp)
	udp.AddChild(NewCloneFileEntry(udp, 0))

	tcp := NewDirEntry("tcp")
	net.AddChild(tcp)
	tcp.AddChild(NewCloneFileEntry(tcp, 0))
}

func main() {
	ctx := context.Background()
	log.SetFlags(0)
	flag.Parse()

	proto := "tcp"
	if strings.HasPrefix(addr, "unix:") {
		proto = "unix"
		addr = addr[5:]
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		log.Fatalln("error listening:", err)
	}
	defer listener.Close()

	for {
		c, err := listener.Accept()
		if err != nil {
			log.Fatalln("error accepting:", err)
			continue
		}

		go func(conn net.Conn) {
			defer conn.Close()

			ctx := context.WithValue(ctx, "conn", conn)
			log.Println("connected", conn.RemoteAddr())

			var handler p9p.HandlerFunc = func(ctx context.Context, msg p9p.Message) (p9p.Message, error) {
				log.Printf("Message: %T %+v\n", msg, msg)

				switch t := msg.(type) {

				case p9p.MessageTversion:
					// TODO whatever version the client wants (for now)
					return msg, nil

				case p9p.MessageTattach:
					// We assume that the first entry is the root and it is a directory
					root, _ := ENTRIES[0].(*DirEntry)
					FIDS[t.Fid] = root.Id.Path
					return p9p.MessageRattach{Qid: *root.Id}, nil

				case p9p.MessageTwalk:
					entry := ENTRIES[FIDS[t.Fid]]
					found := true
					// Perform the walk
					for _, name := range t.Wnames {
						direntry, ok := entry.(*DirEntry)
						if !ok {
							return p9p.MessageRerror{Ename: "Walking from a non-directory is not possible."}, nil
						}

						entry, found = direntry.FindChild(name)
						if !found && !(len(t.Wnames) == 1 && name == "") { // Weird case with 9pr
							return p9p.MessageRerror{Ename: name + " not found"}, nil
						}
					}

					qid := entry.Qid()
					FIDS[t.Newfid] = qid.Path

					// TODO figure out what else should go in the array of qids to return to the client
					return p9p.MessageRwalk{[]p9p.Qid{qid}}, nil

				case p9p.MessageTclunk:
					delete(FIDS, t.Fid)
					return p9p.MessageRclunk{}, nil

				case p9p.MessageTopen:
					entry := ENTRIES[FIDS[t.Fid]]

					clonefile, ok := entry.(*CloneFileEntry)
					if ok {
						clonefile.Clone()
					}

					return p9p.MessageRopen{Qid: entry.Qid(), IOUnit: entry.Size()}, nil
					// TODO at least fail it if the file is not writable
				case p9p.MessageTread:
					entry := ENTRIES[FIDS[t.Fid]]
					offset := t.Offset
					// TODO let each entry type determine how to handle the offset and end
					end := offset + uint64(t.Count)
					contents := entry.Contents()

					if offset > uint64(len(contents)) {
						// TODO confirm what the usual plan9 behaviour is for reading past the end of the file
						return p9p.MessageRread{Data: []byte{}}, nil
					}
					if end > uint64(len(contents)) {
						end = uint64(len(contents))
					}

					return p9p.MessageRread{Data: contents[offset:end]}, nil
				case p9p.MessageTwrite:
					entry := ENTRIES[FIDS[t.Fid]]

					offset := t.Offset
					data := t.Data

					count, err := entry.Write(data, offset)
					if err != nil {
						return p9p.MessageRerror{Ename: err.Error()}, nil
					}

					return p9p.MessageRwrite{Count: count}, nil
				}

				return p9p.MessageRerror{Ename: "Uknown message"}, nil
			}

			var handlerLog p9p.HandlerFunc = func(ctx context.Context, msg p9p.Message) (p9p.Message, error) {
				msg, err := handler(ctx, msg)
				log.Printf("Message: %T %+v\n", msg, msg)
				return msg, err
			}

			err := p9p.ServeConn(ctx, conn, handlerLog)
			if err != nil {
				log.Printf("serving conn: %v", err)
			}
		}(c)
	}
}
