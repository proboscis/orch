package daemon

type panicLogger interface {
	Printf(format string, v ...interface{})
}

func logAndRepanic(logger panicLogger, component string, recovered interface{}) {
	if logger != nil {
		logger.Printf("PANIC in %s: %v", component, recovered)
	}
	panic(recovered)
}
