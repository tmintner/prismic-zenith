package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreServices -framework OSLog

#import <Foundation/Foundation.h>
#import <OSLog/OSLog.h>

typedef struct {
    const char* msg;
    const char* sub;
    const char* cat;
    const char* prc;
    int lvl;
    double ts;
} LogEntryC;

void goLogCallback(LogEntryC entry);

static inline void IterateLogs(double secondsAgo) {
    if (@available(macOS 10.15, *)) {
        NSError *error = nil;
        OSLogStore *store = [OSLogStore storeWithScope:OSLogStoreSystem error:&error];
        if (error) return;

        NSDate *startDate = [NSDate dateWithTimeIntervalSinceNow:-secondsAgo];
        OSLogPosition *position = [store positionWithDate:startDate];

        OSLogEnumerator *enumerator = [store entriesEnumeratorWithOptions:0 position:position predicate:nil error:&error];
        if (error) return;

        for (OSLogEntry *baseEntry in enumerator) {
            if ([baseEntry isKindOfClass:[OSLogEntryLog class]]) {
                OSLogEntryLog *logEntry = (OSLogEntryLog *)baseEntry;

                LogEntryC cEntry;
                cEntry.msg = [logEntry.composedMessage UTF8String];
                cEntry.sub = [logEntry.subsystem UTF8String];
                cEntry.cat = [logEntry.category UTF8String];
                cEntry.prc = [logEntry.process UTF8String];
                cEntry.lvl = (int)logEntry.level;
                cEntry.ts = [logEntry.date timeIntervalSince1970];

                goLogCallback(cEntry);
            }
        }
    }
}
*/
import "C"
import (
	"fmt"
	"time"
)

//export goLogCallback
func goLogCallback(entry C.LogEntryC) {
	fmt.Printf("[%v] [%s] %s: %s\n",
		time.Unix(int64(entry.ts), 0).Format(time.Kitchen),
		C.GoString(entry.prc),
		C.GoString(entry.cat),
		C.GoString(entry.msg),
	)
}

func main() {
	fmt.Println("Starting Native macOS Log Collection Test (last 30 seconds)...")
	C.IterateLogs(30.0)
	fmt.Println("Test Finished.")
}
