package local

import "errors"

var (
	ErrConflict         = errors.New("conflict")
	ErrDownloadingFile  = errors.New("error downloading file")
	ErrRemoteDiffFailed = errors.New("remote diff failed")
)
