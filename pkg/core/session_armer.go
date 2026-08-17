package core

// SessionArmer is implemented by tool executors that need a reference to the
// live [Session] once it exists — for example an executor that owns a
// background process manager and must route its late events (output deltas,
// exit notifications) through the session's event stream. The Rust equivalents
// hold the session directly because the managers live on it; codexgo builds
// executors before the session, so [Spawn] calls ArmSession on every registered
// executor that implements this interface, exactly once, before any turn runs.
type SessionArmer interface {
	ArmSession(sess *Session)
}

// armSessionExecutors invokes ArmSession on every executor of the session's
// [DefaultToolRouter] that implements [SessionArmer]. It is a no-op for other
// routers.
func armSessionExecutors(sess *Session) {
	if sess == nil {
		return
	}
	router, ok := sess.services.ToolRouter.(*DefaultToolRouter)
	if !ok {
		return
	}
	for _, name := range router.order {
		if armer, ok := router.executors[name].(SessionArmer); ok {
			armer.ArmSession(sess)
		}
	}
}
