package router

import "gophermind/gophermind-lib/briefv2/settings"

// eligible applies the need-to-know rule (spec section 6).
//
//	private provider                     always
//	privacy.mode private_only            public providers never
//	privacy.mode need_to_know            public providers for node scope only,
//	                                     or for any scope when the run was
//	                                     started with --allow-public
func (r *Router) eligible(providerName string, scope Scope) bool {
	vis, ok := r.cfg.Visibility(providerName)
	if !ok {
		return false
	}
	if vis == settings.Private {
		return true
	}
	if r.cfg.Privacy.Mode == "private_only" {
		return false
	}
	return scope == ScopeNode || r.allowPublic
}
