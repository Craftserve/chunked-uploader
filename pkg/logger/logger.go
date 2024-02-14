package logger

import (
	"log"
	"os"
)

var logger = log.New(os.Stdout, "[chunked-uploader]: ", log.LstdFlags|log.Lshortfile)

func Log(msg string) {
	logger.Println(msg)
}
