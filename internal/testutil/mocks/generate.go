package mocks

//go:generate go run github.com/matryer/moq@v0.5.3 -pkg mocks -out orchapi_moq.go ../../orchapi OrchAPI
//go:generate go run github.com/matryer/moq@v0.5.3 -pkg mocks -out store_moq.go ../../store Store
//go:generate go run github.com/matryer/moq@v0.5.3 -pkg mocks -out multiplexer_moq.go ../../multiplexer Multiplexer
//go:generate go run github.com/matryer/moq@v0.5.3 -pkg mocks -out git_runner_moq.go ../../git Runner
//go:generate go run github.com/matryer/moq@v0.5.3 -pkg mocks -out github_client_moq.go ../../github Client
