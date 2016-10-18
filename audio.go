package main

import (
	"errors"
	"sync"

	"github.com/docker/go-p9p"
	"github.com/gordonklaus/portaudio"
)

type AudioFileEntry struct {
	mutex  sync.Mutex
	buf    []byte
	writer func()
	Name   string
	Id     *p9p.Qid
}

func NewAudioFileEntry(name string) *AudioFileEntry {
	var audiofile AudioFileEntry
	audiofile.Name = name
	audiofile.Id = &p9p.Qid{Type: p9p.QTFILE, Version: 0, Path: ENTRYCOUNT}
	audiofile.buf = []byte{}
	ENTRIES = append(ENTRIES, &audiofile)
	ENTRYCOUNT++
	return &audiofile
}

func (audiofile *AudioFileEntry) Qid() p9p.Qid {
	return *audiofile.Id
}

func (audiofile *AudioFileEntry) DirStat() *p9p.Dir {
	return &p9p.Dir{
		Qid:    *audiofile.Id,
		Mode:   STDFILEMODE,
		Length: uint64(10 * 1024), // 10K buffer
		Name:   audiofile.Name,
		UID:    "nobody",
		GID:    "nobody",
		MUID:   "nobody",
	}
}

func (audiofile *AudioFileEntry) Size() uint32 {
	return uint32(10 * 1024) // 10K buffer (audio(1) says that the reported file size represents the buffer size)
}

func (audiofile *AudioFileEntry) Read(offset uint64, count uint32) ([]byte, error) {
	return []byte{}, errors.New("microphone is not yet supported")
}

func (audiofile *AudioFileEntry) Write(data []byte, offset uint64) (uint32, error) {
	audiofile.mutex.Lock()
	defer audiofile.mutex.Unlock()
	audiofile.buf = append(audiofile.buf, data...)

	if audiofile.writer == nil {
		audiofile.writer = func() {
			portaudio.Initialize()
			stream, err := portaudio.OpenDefaultStream(0, 2, 44100, 0, func(out [][]int16) {
				audiofile.mutex.Lock()
				defer audiofile.mutex.Unlock()

				if len(audiofile.buf) < 4*len(out[0]) {
					return
				}

				// TODO optimize
				for i := range out[0] {
					right := int16(audiofile.buf[2])
					right |= int16(audiofile.buf[3]) << 8
					left := int16(audiofile.buf[0])
					left |= int16(audiofile.buf[1]) << 8

					pos := i
					out[0][pos] = left
					out[1][pos] = right

					audiofile.buf = audiofile.buf[4:]
				}
			})

			if err != nil {
				panic(err)
			}

			stream.Start()
		}
		go audiofile.writer()
	}

	return uint32(len(data)), nil
}
