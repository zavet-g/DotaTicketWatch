package flights

import "time"

type apiResponse struct {
	Success  bool        `json:"success"`
	Data     []apiTicket `json:"data"`
	Currency string      `json:"currency"`
}

type apiTicket struct {
	Origin             string `json:"origin"`
	Destination        string `json:"destination"`
	OriginAirport      string `json:"origin_airport"`
	DestinationAirport string `json:"destination_airport"`
	Price              int    `json:"price"`
	Airline            string `json:"airline"`
	FlightNumber       string `json:"flight_number"`
	DepartureAt        string `json:"departure_at"`
	ReturnAt           string `json:"return_at"`
	Transfers          int    `json:"transfers"`
	ReturnTransfers    int    `json:"return_transfers"`
	Duration           int    `json:"duration"`
	DurationTo         int    `json:"duration_to"`
	DurationBack       int    `json:"duration_back"`
	Link               string `json:"link"`
}

type Route struct {
	Origin             string
	OriginAirport      string
	DestinationAirport string
	Price              int
	Airline            string
	AirlineName        string
	DurationTo         int
	DepartureAt        time.Time
	Link               string
}

type Snapshot struct {
	Routes    []Route
	UpdatedAt time.Time
	Departure string
	Return    string
}
