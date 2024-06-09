# [Pós GoExpert - FullCycle](https://fullcycle.com.br)

## Desafios técnicos - Tracing distribuído e span (Open Telemetry e Zipkin)

### Pré-requisitos

- [Golang](https://golang.org/)
- [OpenTelemetry](https://opentelemetry.io/docs/languages/go/)
- [Zipkin](https://zipkin.io/)

### Como executar a aplicação

```bash
  # 1 - Clonar o repositório do projeto
  git clone https://github.com/andersonigorf/goexpert-open-telemetry.git

  # 2 - Acessar o diretório do projeto
  cd goexpert-open-telemetry

  # 3 - Executar a aplicação com o Docker
  docker-compose up -d

  ou

  make run
```

### Zipkin

http://localhost:9411

### Exemplos de requisições

```bash
  # 1 - Executar as requisições do arquivo requests.http (dentro da pasta ./api)
  api/requests.http
  
  # 2 - Executar por linha de comando
  curl -X POST -d '{"cep":"<CEP>"}' http://localhost:8080/weather
  
    # HTTP: 200
    curl -X POST -d '{"cep":"29902555"}' http://localhost:8080/weather
  
    # HTTP: 200
    curl -X POST -d '{"cep":"72547240"}' http://localhost:8080/weather
  
    # HTTP: 422 - invalid zipcode
    curl -X POST -d '{"cep":"72547"}' http://localhost:8080/weather
  
    # HTTP: 404 - can not find zipcode
    curl -X POST -d '{"cep":"72547249"}' http://localhost:8080/weather
```