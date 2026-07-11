package model

type Market struct {
	MarketID         string
	ConditionID      string
	EventID          string
	Slug             string
	Title            string
	Symbol           string
	Duration         string
	StartTimeMS      int64
	EndTimeMS        int64
	YesTokenID       string
	NoTokenID        string
	ResolutionSource string
	Active           bool
	Closed           bool
	AcceptingOrders  bool
	ResolvedOutcome  string
	PriceToBeat      string
	FinalPrice       string
	UpdatedAtMS      int64
}

type BookTick struct {
	MarketID, TokenID, Outcome, BestBid, BestAsk, Spread string
	EventTimeMS, ReceivedAtMS                            int64
}

type Trade struct {
	MarketID, TokenID, Outcome, Side, Price, Size, FeeRateBPS string
	EventTimeMS, ReceivedAtMS                                 int64
}

type ReferencePrice struct {
	Source, Symbol, Price     string
	EventTimeMS, ReceivedAtMS int64
}

type Resolution struct {
	MarketID, WinningTokenID, WinningOutcome string
	EventTimeMS                              int64
}
