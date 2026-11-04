package planner

// The reply shapes of the calls whose replies a small model got wrong. A
// json_mode provider has its server enforce them. They require only what the
// parsers need and allow extra fields, so a reply the parser accepts is never
// refused by the schema.

// coverageReplySchema is the reply of the coverage stage: which functions cover
// each requirement, and a root test (a shell command) for each acceptance one.
const coverageReplySchema = `{"type":"object","required":["map","root_tests"],"properties":{
 "map":{"type":"array","items":{"type":"object","required":["requirement","nodes"],"properties":{
  "requirement":{"type":"string"},"nodes":{"type":"array","items":{"type":"string"}}}}},
 "root_tests":{"type":"array","items":{"type":"object","required":["requirement","name","command"],"properties":{
  "requirement":{"type":"string"},"name":{"type":"string"},"given":{"type":"string"},
  "expect":{"type":"string"},"command":{"type":"string"}}}}}}`
