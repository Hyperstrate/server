package main

import (
	_ "hyperstrate/server/docs"
	"hyperstrate/server/internal/app"
	"hyperstrate/server/internal/shared/logger"
)

// @title AI Proxy API
// @version 1.0
// @description AI model proxy with async job support. Powered by Gin, Fx, GORM, and AWS Lambda.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	logger.Init()
	app.NewHTTPApp().Run()
}
