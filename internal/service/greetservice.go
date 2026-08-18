package service

type GreetService struct{}

func NewGreetService() *GreetService {
	return &GreetService{}
}

func (g *GreetService) Greet(name string) string {
	return "Hello " + name + "!"
}
