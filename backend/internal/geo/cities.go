// Package geo holds the canonical list of supported cities.
package geo

// City is a supported city with its relative weight in seeded data.
type City struct {
	Name   string
	Weight float64
}

// Cities is the canonical city list.
var Cities = []City{
	{"İstanbul", 0.35}, {"Ankara", 0.12}, {"İzmir", 0.10}, {"Bursa", 0.06},
	{"Antalya", 0.06}, {"Adana", 0.04}, {"Konya", 0.04}, {"Gaziantep", 0.03},
	{"Kocaeli", 0.04}, {"Mersin", 0.03}, {"Eskişehir", 0.03}, {"Trabzon", 0.03},
	{"Kayseri", 0.03}, {"Samsun", 0.04},
}

// Names returns the city names in canonical order.
func Names() []string {
	out := make([]string, len(Cities))
	for i, c := range Cities {
		out[i] = c.Name
	}
	return out
}
