// Historical tariff snapshot from legacy openai-pricing.js, not current pricing advice.
package parser

type historicalPrice struct{ Input, Output float64 }

var historicalPrices = map[string]historicalPrice{
	"gpt-6-luna":    {0.1, 0.5},
	"gpt-6.1-sol":   {2, 10},
	"gpt-6-sol":     {2, 10},
	"gpt-6-astra":   {10, 50},
	"gpt-5.6-terra": {2, 12},
	"gpt-5.6-luna":  {0.2, 1.2},
	"gpt-5.6-sol":   {4, 20},
	"gpt-5.6-cyber": {12.5, 75},
	"gpt-5.4":       {2.5, 15},
	"gpt-5.4-mini":  {0.75, 4.5},
	"gpt-5.4-nano":  {0.2, 1.25},
	"gpt-5.4-pro":   {30, 180},
	"gpt-5.3-codex": {1.75, 14},
	"gpt-5.2":       {1.75, 14},
	"gpt-5.2-pro":   {21, 168},
	"gpt-5.1":       {1.25, 10},
	"gpt-5":         {1.25, 10},
	"gpt-5-mini":    {0.25, 2},
	"gpt-5-nano":    {0.05, 0.4},
	"gpt-4.1":       {2, 8},
	"gpt-4.1-mini":  {0.4, 1.6},
	"gpt-4.1-nano":  {0.1, 0.4},
	"gpt-4o":        {2.5, 10},
	"gpt-4o-mini":   {0.15, 0.6},
	"o3":            {2, 8},
	"o3-mini":       {1.1, 4.4},
	"o3-pro":        {20, 80},
	"o4-mini":       {1.1, 4.4},
	"o1":            {15, 60},
	"o1-mini":       {1.1, 4.4},
	"o1-pro":        {150, 600},
	"gpt-5.6":       {4, 20},
}
