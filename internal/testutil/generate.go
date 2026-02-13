package testutil

//go:generate go run github.com/matryer/moq -out mock_orchapi_test.go -pkg testutil ../orchapi OrchAPI
//go:generate go run github.com/matryer/moq -out mock_store_test.go -pkg testutil ../store Store
//go:generate go run github.com/matryer/moq -out mock_multiplexer_test.go -pkg testutil ../multiplexer Multiplexer
//go:generate go run github.com/matryer/moq -out mock_git_runner_test.go -pkg testutil ../git Runner
//go:generate go run github.com/matryer/moq -out mock_github_client_test.go -pkg testutil ../github Client
