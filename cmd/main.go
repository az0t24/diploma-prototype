package main

import (
	efwrapperv1 "diploma-prototype/api/efwrapper/v1"
	"diploma-prototype/internal/grpc"
	"diploma-prototype/internal/http"
	"diploma-prototype/internal/settings"
)

func main() {
	s := settings.GetSettings()

	updatesChan := make(chan *efwrapperv1.Configuration, 1)
	grpcServer := grpc.NewService(updatesChan, s)
	go grpcServer.Run()

	httpServer := http.NewServer(updatesChan, s)
	go httpServer.Run()

	//todo: add errors & signals handling
	select {}
}
