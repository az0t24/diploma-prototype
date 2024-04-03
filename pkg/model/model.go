package model

import (
	"google.golang.org/protobuf/types/known/anypb"
	"istio.io/api/networking/v1alpha3"
)

func EnvoyFiltersToAny(ef []*v1alpha3.EnvoyFilter) []*anypb.Any {
	arr := make([]*anypb.Any, 0, len(ef))
	for _, e := range ef {
		a, _ := anypb.New(e)
		arr = append(arr, a)
	}
	return arr
}

type MetaData struct {
	Name      string `json:"name"`
	NameSpace string `json:"namespace"`
}
