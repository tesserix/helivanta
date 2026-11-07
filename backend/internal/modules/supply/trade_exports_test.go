package supply

// Protocol integration tests exercise the real owner store without adding a runtime API.
var TestRelationshipLock = relationshipLock
var TestLoadTradeRelationship = loadTradeRelationship
var TestSaveTradeRelationship = saveTradeRelationship
var TestEnqueueRelationshipExchange = enqueueRelationshipExchange

const TestRelationshipExchangeSubject = relationshipExchangeSubject
