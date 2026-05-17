package main

import (
	"context"
	"log/slog"
	"os"

	"hyperstrate/server/docs"
	"hyperstrate/server/internal/app"
	"hyperstrate/server/internal/shared/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	"github.com/gin-gonic/gin"
)

var ginLambda *ginadapter.GinLambdaV2

func init() {
	logger.Init()

	docs.SwaggerInfo.Host = ""
	docs.SwaggerInfo.Schemes = []string{"https"}

	var router *gin.Engine
	application := app.NewLambdaApp(&router)
	if err := application.Start(context.Background()); err != nil {
		slog.Error("failed to start fx app", "err", err)
		os.Exit(1)
	}
	ginLambda = ginadapter.NewV2(router)
}

func handler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return ginLambda.ProxyWithContext(ctx, request)
}

func main() {
	lambda.Start(handler)
}
