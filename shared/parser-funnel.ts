export const funnelStages=[
 {id:'received',title:'Получено',color:'#2563eb'},
 {id:'accepted',title:'Прошло отбор',color:'#7c3aed'},
 {id:'fulltext',title:'Получен полный текст',color:'#0891b2'},
 {id:'structured',title:'Извлечены поля',color:'#b45309'},
 {id:'roles',title:'Определены роли',color:'#c026d3'},
 {id:'published',title:'Опубликовано',color:'#15803d'},
] as const;
export type FunnelStage=typeof funnelStages[number]['id'];
export type FunnelCounts=Record<FunnelStage,number>;
export interface ParserFunnelReport {
 checkedAt:number;timezone:'Europe/Moscow';from:string;to:string;days:number;
 filters:{sourceId:string;parser:string};
 sourceOptions:{id:string;name:string}[];parserOptions:{id:string;name:string}[];
 daily:({day:string}&FunnelCounts)[];totals:FunnelCounts;averages:FunnelCounts;
 cohort:FunnelCounts&{rejected:number;error:number;waiting:number;created:number;linked:number;undatedPublished:number};
}
