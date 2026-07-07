package daemon

import (
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

func (s *SocketServer) appendArtifactEventBestEffort(st store.Store, ref *model.RunRef, kind string, attrs map[string]string) {
	if err := st.AppendEvent(ref, model.NewArtifactEvent(kind, attrs)); err != nil {
		s.logArtifactAppendError(ref, kind, err)
	}
}

func (s *SocketServer) logArtifactAppendError(ref *model.RunRef, kind string, err error) {
	if err == nil || s == nil || s.logger == nil {
		return
	}
	runRef := "<nil>"
	if ref != nil {
		runRef = ref.String()
	}
	s.logger.Printf("%s: failed to record %s artifact: %v", runRef, kind, err)
}
