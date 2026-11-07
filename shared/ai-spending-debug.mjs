// Сериализация локального read-only ledger; никаких запросов к ИИ.
// Все денежные суммы относятся только к provider=openai/openrouter.
export function aiSpendingSnapshot(report,budgets) {
  if (!report || !report.day || !report.month || !report.providers) {
    throw new Error('ai_spending_unavailable');
  }
  return {
    dataAsOf:report.checkedAt,
    summary:{
      dayBudgetUsd:budgets.dayBudgetUsd,
      monthBudgetUsd:budgets.monthBudgetUsd,
      dayStart:report.dayStart,
      monthStart:report.monthStart,
      dayConfirmedUsd:report.day.confirmedUsd,
      dayUnknownReservedUsd:report.day.unknownReservedUsd,
      dayUpperEstimateUsd:report.day.upperEstimateUsd,
      monthConfirmedUsd:report.month.confirmedUsd,
      monthUnknownReservedUsd:report.month.unknownReservedUsd,
      monthUpperEstimateUsd:report.month.upperEstimateUsd,
      monthUnknownCostOperations:report.month.unknownCostOperations,
      monthUnpricedOperations:report.month.unpricedOperations,
      modelBreakdownAvailable:Array.isArray(report.models),
      duplicateAICallsAvailable:report.duplicateAICallsAvailable===true,
      providers:report.providers,
    },
    filters:{},
    items:report.latest,
    total:report.total,
    coverage:'server_latest',
    breakdown:report.models,
  };
}
