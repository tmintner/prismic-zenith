//go:build windows

package collector

import (
	"encoding/xml"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"zenith/pkg/db"

	"golang.org/x/sys/windows"
)

var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")

	procEvtQuery  = modwevtapi.NewProc("EvtQuery")
	procEvtClose  = modwevtapi.NewProc("EvtClose")
	procEvtNext   = modwevtapi.NewProc("EvtNext")
	procEvtRender = modwevtapi.NewProc("EvtRender")
)

const (
	EvtQueryChannelPath      = 0x1
	EvtQueryReverseDirection = 0x200
	EvtRenderEventXml        = 1
)

func EvtQuery(session windows.Handle, path *uint16, query *uint16, flags uint32) (windows.Handle, error) {
	r0, _, e1 := syscall.Syscall6(procEvtQuery.Addr(), 4, uintptr(session), uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(query)), uintptr(flags), 0, 0)
	handle := windows.Handle(r0)
	if handle == 0 {
		if e1 != 0 {
			return 0, error(e1)
		}
		return 0, syscall.EINVAL
	}
	return handle, nil
}

func EvtClose(handle windows.Handle) error {
	r1, _, e1 := syscall.Syscall(procEvtClose.Addr(), 1, uintptr(handle), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}

func EvtNext(resultSet windows.Handle, eventArraySize uint32, eventArray *windows.Handle, timeout uint32, flags uint32, returned *uint32) error {
	r1, _, e1 := syscall.Syscall6(procEvtNext.Addr(), 6, uintptr(resultSet), uintptr(eventArraySize), uintptr(unsafe.Pointer(eventArray)), uintptr(timeout), uintptr(flags), uintptr(unsafe.Pointer(returned)))
	if r1 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}

func EvtRender(context windows.Handle, fragment windows.Handle, flags uint32, bufferSize uint32, buffer *uint16, bufferUsed *uint32, propertyCount *uint32) error {
	r1, _, e1 := syscall.Syscall9(procEvtRender.Addr(), 7, uintptr(context), uintptr(fragment), uintptr(flags), uintptr(bufferSize), uintptr(unsafe.Pointer(buffer)), uintptr(unsafe.Pointer(bufferUsed)), uintptr(unsafe.Pointer(propertyCount)), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}

// Windows Event Log XML Structure
type WinEventXML struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     int `xml:"EventID"`
		Level       int `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
	RenderingInfo struct {
		Message string `xml:"Message"` // Needs explicit rendering usually
	} `xml:"RenderingInfo"`
}

func CollectLogs(database *db.VictoriaDB, duration string) error {
	// Query channels "System" and "Application" for recent events
	channels := []string{"System", "Application"}

	// Calculate start time based on duration (simple approximation for query)
	// Real query syntax: *[System[TimeCreated[timediff(@SystemTime) <= 300000]]] (300000ms = 5m)
	// We'll simplify to just getting the last N records if timediff is hard in pure query,
	// but XPath 1.0 subset in EvtQuery supports timediff.

	// Default 5m = 300000ms
	ms := int64(300000)
	dur, err := time.ParseDuration(duration)
	if err == nil {
		ms = dur.Milliseconds()
	}

	query := fmt.Sprintf("*[System[TimeCreated[timediff(@SystemTime) <= %d]]]", ms)

	for _, channel := range channels {
		if err := collectChannelLogs(database, channel, query); err != nil {
			// Log error but continue to next channel
			fmt.Printf("failed to collect logs from channel %s: %v\n", channel, err)
		}
	}

	return nil
}

func collectChannelLogs(database *db.VictoriaDB, channel, query string) error {
	path, _ := syscall.UTF16PtrFromString(channel)
	q, _ := syscall.UTF16PtrFromString(query)

	hSubscription, err := EvtQuery(0, path, q, EvtQueryChannelPath|EvtQueryReverseDirection)
	if err != nil {
		return fmt.Errorf("EvtQuery failed: %v", err)
	}
	defer EvtClose(hSubscription)

	events := make([]windows.Handle, 10)
	var returned uint32

	for {
		err := EvtNext(hSubscription, uint32(len(events)), &events[0], 2000, 0, &returned)
		if err == windows.ERROR_NO_MORE_ITEMS {
			break
		}
		if err != nil {
			return fmt.Errorf("EvtNext failed: %v", err)
		}

		for i := 0; i < int(returned); i++ {
			eventHandle := events[i]
			defer EvtClose(eventHandle)

			xmlContent, err := renderEventXML(eventHandle)
			if err != nil {
				continue
			}

			// Parse XML
			var event WinEventXML
			if err := xml.Unmarshal([]byte(xmlContent), &event); err != nil {
				continue
			}

			// Format for VictoriaLogs
			entry := db.LogEntry{
				Timestamp:   event.System.TimeCreated.SystemTime,
				ProcessName: event.System.Provider.Name,
				Category:    fmt.Sprintf("EventID: %d", event.System.EventID),
				LogLevel:    fmt.Sprintf("Level: %d", event.System.Level),
				// Message rendering requires a publisher metadata handle which is complex.
				// We'll use the provider name and EventID as the core message for now,
				// or if RenderingInfo is present (rare without explicit format render).
				EventMessage: fmt.Sprintf("EventID %d from %s", event.System.EventID, event.System.Provider.Name),
			}
			database.InsertLog(entry)
		}
	}
	return nil
}

func renderEventXML(event windows.Handle) (string, error) {
	var bufferSize uint32
	var propertyCount uint32

	// EvtRender with EvtRenderEventXml (1)
	err := EvtRender(0, event, EvtRenderEventXml, 0, nil, &bufferSize, &propertyCount)
	if err != windows.ERROR_INSUFFICIENT_BUFFER {
		return "", err
	}

	buffer := make([]uint16, bufferSize/2)
	err = EvtRender(0, event, EvtRenderEventXml, bufferSize, &buffer[0], &bufferSize, &propertyCount)
	if err != nil {
		return "", err
	}

	return syscall.UTF16ToString(buffer), nil
}
