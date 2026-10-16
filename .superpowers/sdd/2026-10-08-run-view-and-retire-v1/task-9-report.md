# Task 9 report
BASE 24cf465. Client now speaks the node contract: PipelineRuns/State/Node/Artifact/Decisions/Report/Events, SchemaMismatchError (header, and body schema_version except report), Event.ID, LastID, Header, six typed decoders (NodeStatus carries revision, C7).
Rulings applied: C6 (final golden: len(Revised)==1, schema via report.SchemaVersion), C15 (sameJSON now diffs key sets per object level with the path, then values), C5 (decisions golden has 2 records, revision decision second), stale hub/pipelineEventsHandler comments reworded (sse.go rewritten; ui/pipelinepump_test.go comment).
Added: 503 stream-cap test. serve/events.go: removed Task*/WaveChanged/RunReport event types.
ApplyNodeStatus does not exist yet (ui is Task 10); the client only decodes Revision.
Tests: gt ./client/... -race ok; gt ./gophermind-lib/serve/... ok; gofmt clean; go vet ./client clean. ui/main not built (per brief).
