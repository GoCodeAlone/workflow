// Package examples contains complete examples of AI-generated workflows.
package examples

import "github.com/GoCodeAlone/workflow/ai"

// StockTradingRequest is the example user intent for stock price monitoring.
var StockTradingRequest = ai.GenerateRequest{
	Intent: "Alert me when AAPL stock price changes by 5% from its opening price, then trigger a buy if it drops or a sell if it rises.",
	Context: map[string]string{
		"stock":     "AAPL",
		"threshold": "5%",
		"api":       "Alpha Vantage or similar stock price API",
	},
	Constraints: []string{
		"Poll every minute during market hours",
		"Track percentage change from daily open",
		"Send alerts via messaging system",
		"Use state machine for buy/sell decision flow",
	},
}

// StockTradingResponse is the expected generated response for the stock trading example.
// This serves as a reference for tests and documentation.
var StockTradingResponse = ai.GenerateResponse{
	Explanation: `This workflow monitors AAPL stock price every minute during market hours.
When the price changes by 5% from the daily open, it triggers a state machine
that decides whether to buy (price dropped) or sell (price rose). Alerts are
sent through the messaging system at each stage.

Components:
1. Schedule trigger polls every minute
2. StockPriceChecker (custom) fetches current price from stock API
3. Event processor detects 5% change threshold
4. State machine manages buy/sell decision flow
5. TradeExecutor (custom) executes the trade
6. Messaging handlers send alerts at each transition`,
	Components: []ai.ComponentSpec{
		{
			Name:        "stock-price-checker",
			Type:        "stock.price.checker",
			Description: "Fetches current stock price from an external API and compares against the daily opening price to detect percentage changes.",
			Interface:   "modular.Module",
			GoCode: `package module

// [DEMO STUB] Replace this entire file with a real stock API integration.
// The CheckPrice method below returns synthetic demo data.
// In production, call a real API (e.g. Alpha Vantage, Polygon.io) and
// parse the JSON response to get live prices.

import (
	"context"
	"math"
	"sync"

	"github.com/GoCodeAlone/modular"
)

type StockPriceChecker struct {
	name       string
	symbol     string
	apiKey     string
	openPrice  float64
	lastPrice  float64
	mu         sync.RWMutex
	httpClient *http.Client
}

func NewStockPriceChecker(name, symbol, apiKey string) *StockPriceChecker {
	return &StockPriceChecker{
		name:       name,
		symbol:     symbol,
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

func (s *StockPriceChecker) Name() string { return s.name }

func (s *StockPriceChecker) RegisterConfig(app modular.Application) {}

func (s *StockPriceChecker) Init(app modular.Application) error {
	return nil
}

func (s *StockPriceChecker) CheckPrice(ctx context.Context) (currentPrice float64, pctChange float64, err error) {
	// [DEMO STUB] This is example/demonstration code only.
	// In production, replace this method body with a real stock API call,
	// e.g. Alpha Vantage: GET /query?function=GLOBAL_QUOTE&symbol=AAPL&apikey=YOUR_KEY
	// For now, synthetic demo prices are returned so the workflow exercises the full decision path.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.openPrice == 0 {
		// Seed with a realistic demo opening price so callers always get usable data.
		s.openPrice = 182.50 // [DEMO] hardcoded AAPL-like opening price
		s.lastPrice = 182.50
	}

	// Simulate a small price tick so repeated calls show movement.
	s.lastPrice += 0.10 // [DEMO] synthetic +0.10 tick per call

	pctChange = ((s.lastPrice - s.openPrice) / s.openPrice) * 100
	return s.lastPrice, pctChange, nil
}

func (s *StockPriceChecker) SetPrices(openPrice, currentPrice float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openPrice = openPrice
	s.lastPrice = currentPrice
}

func (s *StockPriceChecker) ThresholdExceeded(threshold float64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.openPrice == 0 {
		return false
	}
	pctChange := ((s.lastPrice - s.openPrice) / s.openPrice) * 100
	return math.Abs(pctChange) >= threshold
}

// Ensure interface compliance
var _ modular.Module = (*StockPriceChecker)(nil)
`,
		},
		{
			Name:        "trade-executor",
			Type:        "trade.executor",
			Description: "Executes buy or sell trades based on state machine decisions. Logs trade actions and sends confirmation messages.",
			Interface:   "modular.Module",
			GoCode: `package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

type TradeAction string

const (
	TradeActionBuy  TradeAction = "buy"
	TradeActionSell TradeAction = "sell"
	TradeActionHold TradeAction = "hold"
)

type TradeOrder struct {
	Symbol    string      ` + "`json:\"symbol\"`" + `
	Action    TradeAction ` + "`json:\"action\"`" + `
	Quantity  int         ` + "`json:\"quantity\"`" + `
	Price     float64     ` + "`json:\"price\"`" + `
	Timestamp time.Time   ` + "`json:\"timestamp\"`" + `
}

type TradeResult struct {
	OrderID   string      ` + "`json:\"orderId\"`" + `
	Status    string      ` + "`json:\"status\"`" + `
	Action    TradeAction ` + "`json:\"action\"`" + `
	Price     float64     ` + "`json:\"price\"`" + `
	Timestamp time.Time   ` + "`json:\"timestamp\"`" + `
}

type TradeExecutor struct {
	name   string
	logger modular.Logger
}

func NewTradeExecutor(name string) *TradeExecutor {
	return &TradeExecutor{name: name}
}

func (t *TradeExecutor) Name() string { return t.name }

func (t *TradeExecutor) RegisterConfig(app modular.Application) {}

func (t *TradeExecutor) Init(app modular.Application) error {
	t.logger = app.Logger()
	return nil
}

func (t *TradeExecutor) Execute(ctx context.Context, order TradeOrder) (*TradeResult, error) {
	t.logger.Info(fmt.Sprintf("Executing %s order for %s: qty=%d price=%.2f",
		order.Action, order.Symbol, order.Quantity, order.Price))

	// [DEMO STUB] Returns a simulated order confirmation.
	// In production, replace this with a real brokerage API call (e.g. Alpaca, TD Ameritrade).
	return &TradeResult{
		OrderID:   fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
		Status:    "executed",
		Action:    order.Action,
		Price:     order.Price,
		Timestamp: time.Now(),
	}, nil
}

var _ modular.Module = (*TradeExecutor)(nil)
`,
		},
	},
}

// DynamicStockPriceCheckerSource is the stock price checker component written
// in dynamic format (package component, stdlib only) for the Yaegi interpreter.
const DynamicStockPriceCheckerSource = `package component

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

var openPrice float64
var lastPrice float64

func Name() string {
	return "stock-price-checker"
}

func Init(services map[string]interface{}) error {
	// Seed with a mock opening price
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	openPrice = 150.0 + r.Float64()*10.0
	lastPrice = openPrice
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	symbol, _ := params["symbol"].(string)
	if symbol == "" {
		return nil, fmt.Errorf("missing required parameter: symbol")
	}

	thresholdVal, _ := params["threshold"].(float64)
	if thresholdVal == 0 {
		thresholdVal = 5.0
	}

	// Simulate price movement
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	change := (r.Float64() - 0.5) * 4.0
	lastPrice = lastPrice + change

	pctChange := ((lastPrice - openPrice) / openPrice) * 100.0
	exceeded := math.Abs(pctChange) >= thresholdVal

	signal := "hold"
	if exceeded && pctChange < 0 {
		signal = "buy"
	} else if exceeded && pctChange > 0 {
		signal = "sell"
	}

	return map[string]interface{}{
		"symbol":     strings.ToUpper(symbol),
		"openPrice":  fmt.Sprintf("%.2f", openPrice),
		"lastPrice":  fmt.Sprintf("%.2f", lastPrice),
		"pctChange":  fmt.Sprintf("%.2f", pctChange),
		"exceeded":   exceeded,
		"signal":     signal,
		"timestamp":  time.Now().Format(time.RFC3339),
	}, nil
}
`

// DynamicTradeExecutorSource is the trade executor component written
// in dynamic format (package component, stdlib only) for the Yaegi interpreter.
const DynamicTradeExecutorSource = `package component

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func Name() string {
	return "trade-executor"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}
	symbol, _ := params["symbol"].(string)
	if symbol == "" {
		return nil, fmt.Errorf("missing required parameter: symbol")
	}
	price, _ := params["price"].(string)

	orderID := fmt.Sprintf("ORD-%d", time.Now().UnixNano())

	return map[string]interface{}{
		"orderId":   orderID,
		"status":    "executed",
		"action":    strings.ToLower(action),
		"symbol":    strings.ToUpper(symbol),
		"price":     price,
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}
`

// DynamicStockTradingComponents provides the stock trading components
// in dynamic format suitable for deployment via the DeployService.
var DynamicStockTradingComponents = []ai.ComponentSpec{
	{
		Name:        "stock-price-checker",
		Type:        "stock.price.checker",
		Description: "Fetches current stock price using mock data and detects percentage changes from opening price.",
		Interface:   "dynamic",
		GoCode:      DynamicStockPriceCheckerSource,
	},
	{
		Name:        "trade-executor",
		Type:        "trade.executor",
		Description: "Executes buy or sell trades and returns order confirmation.",
		Interface:   "dynamic",
		GoCode:      DynamicTradeExecutorSource,
	},
}

// StockTradingYAML is the expected workflow YAML config for the stock trading example.
const StockTradingYAML = `# Stock Trading Alert Workflow
# Monitors AAPL price, alerts on 5% change, triggers buy/sell decisions

modules:
  # HTTP server for webhook/API access
  - name: http-server
    type: http.server
    config:
      address: ":8080"

  - name: api-router
    type: http.router
    dependsOn:
      - http-server

  - name: trade-api
    type: api.handler
    config:
      resourceName: "trades"
    dependsOn:
      - api-router

  # Stock price monitoring (custom component)
  - name: price-checker
    type: stock.price.checker
    config:
      symbol: "AAPL"
      threshold: 5.0

  # Event processing for price change detection
  - name: price-event-processor
    type: event.processor
    config:
      bufferSize: 500
      cleanupInterval: "1m"

  # Messaging for alerts
  - name: alert-broker
    type: messaging.broker

  - name: price-alert-handler
    type: messaging.handler
    config:
      description: "Sends price change alerts"

  - name: trade-notification-handler
    type: messaging.handler
    config:
      description: "Sends trade execution notifications"

  # State machine for buy/sell decisions
  - name: trade-decision-engine
    type: statemachine.engine
    config:
      description: "Manages buy/sell decision flow"

  # Trade execution (custom component)
  - name: trade-executor
    type: trade.executor
    config:
      description: "Executes trades via brokerage API"

workflows:
  # HTTP routes
  http:
    routes:
      - method: GET
        path: /api/trades
        handler: trade-api
      - method: POST
        path: /api/trades
        handler: trade-api

  # Event processing for price changes
  event:
    processor: price-event-processor
    patterns:
      - patternId: "price-drop-alert"
        eventTypes: ["stock.price.drop"]
        windowTime: "1m"
        condition: "count"
        minOccurs: 1
      - patternId: "price-rise-alert"
        eventTypes: ["stock.price.rise"]
        windowTime: "1m"
        condition: "count"
        minOccurs: 1
    handlers:
      - patternId: "price-drop-alert"
        handler: price-alert-handler
      - patternId: "price-rise-alert"
        handler: price-alert-handler

  # Messaging subscriptions
  messaging:
    subscriptions:
      - topic: price-alerts
        handler: price-alert-handler
      - topic: trade-notifications
        handler: trade-notification-handler

  # State machine for trade decisions
  statemachine:
    engine: trade-decision-engine
    definitions:
      - name: trade-decision
        description: "Stock trade decision workflow"
        initialState: "monitoring"
        states:
          monitoring:
            description: "Monitoring price changes"
            isFinal: false
          threshold_exceeded:
            description: "Price threshold exceeded, evaluating"
            isFinal: false
          buy_signal:
            description: "Buy signal detected (price dropped)"
            isFinal: false
          sell_signal:
            description: "Sell signal detected (price rose)"
            isFinal: false
          executing_trade:
            description: "Trade is being executed"
            isFinal: false
          trade_completed:
            description: "Trade executed successfully"
            isFinal: true
          trade_failed:
            description: "Trade execution failed"
            isFinal: true
            isError: true
          cooldown:
            description: "Cooling down after trade"
            isFinal: false
        transitions:
          detect_change:
            fromState: "monitoring"
            toState: "threshold_exceeded"
          signal_buy:
            fromState: "threshold_exceeded"
            toState: "buy_signal"
          signal_sell:
            fromState: "threshold_exceeded"
            toState: "sell_signal"
          execute_buy:
            fromState: "buy_signal"
            toState: "executing_trade"
          execute_sell:
            fromState: "sell_signal"
            toState: "executing_trade"
          trade_success:
            fromState: "executing_trade"
            toState: "trade_completed"
          trade_error:
            fromState: "executing_trade"
            toState: "trade_failed"
          resume_monitoring:
            fromState: "trade_completed"
            toState: "cooldown"
          cooldown_complete:
            fromState: "cooldown"
            toState: "monitoring"
    hooks:
      - workflowType: "trade-decision"
        transitions: ["execute_buy", "execute_sell"]
        handler: "trade-executor"
      - workflowType: "trade-decision"
        toStates: ["trade_completed", "trade_failed"]
        handler: "trade-notification-handler"

triggers:
  # Poll every minute during market hours
  schedule:
    jobs:
      - cron: "* 9-16 * * 1-5"
        workflow: "trade-decision"
        action: "check_price"
        params:
          symbol: "AAPL"
          threshold: 5.0
`
