// Optional server-owned automatic-parser budget hook. Manual AI features do not
// receive it. The hook sees request sizes/pricing, never writes content to logs.
export const withSchedulerBilling=(env,request,work)=>env.SCHEDULER_BILLING?env.SCHEDULER_BILLING.run(request,work):work();
