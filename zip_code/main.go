package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.23.1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"
)

type ZipCode struct {
	CEP string `json:"cep"`
}

const (
	WeatherApiUrl = "http://goapp-weather:8181/weather"

	MethodNotAllowed    = "method not allowed"
	UnprocessibleEntity = "invalid zipcode"
	InternalServerError = "error while searching for weather"
	InvalidJson         = "invalid json"
)

// load env vars cfg
func init() {
	viper.AutomaticEnv()
}

func initProvider() {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(viper.GetString("OTEL_SERVICE_NAME")),
		),
	)
	if err != nil {
		log.Fatalf("failed to create resource: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, viper.GetString("OTEL_EXPORTER_OTLP_ENDPOINT"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("failed to create gRPC connection to collector: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		log.Fatalf("failed to create trace exporter: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
}

func main() {
	initProvider()
	http.HandleFunc("/weather", HandleRequest)
	fmt.Println("Starting web server on port" + viper.GetString("HTTP_PORT"))
	err := http.ListenAndServe(viper.GetString("HTTP_PORT"), nil)
	if err != nil {
		return
	}
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	tr := otel.Tracer(viper.GetString("OTEL_SERVICE_NAME"))
	ctx, span := tr.Start(context.Background(), viper.GetString("REQUEST_NAME_OTEL"))
	defer span.End()

	if r.Method != http.MethodPost {
		http.Error(w, MethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	var requestData ZipCode
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		http.Error(w, InvalidJson, http.StatusBadRequest)
		return
	}

	validate := regexp.MustCompile(`^[0-9]{5}-?[0-9]{3}$`)
	if !validate.MatchString(requestData.CEP) {
		http.Error(w, UnprocessibleEntity, http.StatusUnprocessableEntity)
		return
	}

	resp, err := searchWeather(ctx, requestData)
	if err != nil {
		http.Error(w, InternalServerError, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		http.Error(w, InternalServerError, http.StatusInternalServerError)
		return
	}
}

func searchWeather(ctx context.Context, requestData ZipCode) (*http.Response, error) {
	tr := otel.Tracer(viper.GetString("OTEL_SERVICE_NAME"))
	_, span := tr.Start(ctx, viper.GetString("REQUEST_NAME_OTEL")+" - searchWeather")
	span.SetAttributes(attribute.String("cep", requestData.CEP))
	defer span.End()

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return nil, err
	}

	log.Printf("Requesting URL: %s", WeatherApiUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, WeatherApiUrl, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	return client.Do(req)
}
