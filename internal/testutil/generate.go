package testutil

//go:generate go run github.com/matryer/moq -out mock_orchapi.go -pkg testutil ../orchapi OrchAPI
//go:generate go run github.com/matryer/moq -out mock_store.go -pkg testutil ../store Store
//go:generate go run github.com/matryer/moq -out mock_multiplexer.go -pkg testutil ../multiplexer Multiplexer
//go:generate go run github.com/matryer/moq -out mock_git_runner.go -pkg testutil ../git Runner
//go:generate go run github.com/matryer/moq -out mock_github_client.go -pkg testutil ../github Client
