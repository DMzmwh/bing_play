package service

import "github.com/wailsapp/wails/v3/pkg/application"

type GreetService struct{}

func NewGreetService() *GreetService {
	return &GreetService{}
}

func NewGreetWailsService(s *GreetService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
	}
}
func (g *GreetService) Greet(name string) string {
	return "Hello " + name + "!"
}
