package seed

// Scenario identifies a hand-crafted user by e-mail address.
type Scenario string

const (
	ScenarioAbandonedAfterPurchase Scenario = "scenario-abandoned-after-purchase@example.test"
	ScenarioPurchasedAfterCart     Scenario = "scenario-purchased-after-cart@example.test"
	ScenarioAnkaraAbandoned        Scenario = "scenario-ankara-abandoned@example.test"
	ScenarioOldCart                Scenario = "scenario-old-cart@example.test"
	ScenarioNoSMSConsent           Scenario = "scenario-no-sms-consent@example.test"
	ScenarioEmptyName              Scenario = "scenario-empty-name@example.test"
	ScenarioLongestName            Scenario = "scenario-longest-name@example.test"
)

type scenarioEvent struct {
	typ     string
	daysAgo int
}

type scenarioDef struct {
	email      Scenario
	city       string
	firstName  string
	smsConsent bool
	events     []scenarioEvent
}

var scenarioDefs = []scenarioDef{
	{ScenarioAbandonedAfterPurchase, "İstanbul", "Ayşe", true, []scenarioEvent{{"purchase", 20}, {"cart_add", 5}}},
	{ScenarioPurchasedAfterCart, "İstanbul", "Mehmet", true, []scenarioEvent{{"cart_add", 10}, {"purchase", 8}}},
	{ScenarioAnkaraAbandoned, "Ankara", "Elif", true, []scenarioEvent{{"cart_add", 3}}},
	{ScenarioOldCart, "İstanbul", "Can", true, []scenarioEvent{{"cart_add", 40}}},
	{ScenarioNoSMSConsent, "İstanbul", "Zeynep", false, []scenarioEvent{{"cart_add", 2}}},
	{ScenarioEmptyName, "İstanbul", "", true, []scenarioEvent{{"cart_add", 4}}},
	{ScenarioLongestName, "İstanbul", longestFirstName, true, []scenarioEvent{{"cart_add", 6}}},
}
