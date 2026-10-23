export type EntityType = 'company' | 'agency' | 'channel';
export interface Session {
    name: string;
    role: 'admin' | 'user';
    isAdmin: boolean;
}
export interface Office {
    address: string;
    metro: string;
    lat?: number;
    lon?: number;
    [key: string]: unknown;
}
export interface Entity {
    id?: number;
    name: string;
    entityType: EntityType;
    rank?: number | null;
    favorite?: boolean;
    hidden?: boolean;
    hasVacancies?: boolean;
    hasApplications?: boolean;
    applicationStatus?: string | null;
    offices?: Office[];
    middle?: number[];
    senior?: number[];
    lead?: number[];
    head?: number[];
    deferment?: string;
    rating?: number | null;
    wlbScore?: number | null;
    score?: number;
    hasVacancySource?: boolean;
    sector?: string;
    roles?: string;
    confidence?: string;
    vacancyCount?: number;
    revenue?: Record<string, unknown>;
    reviewsScore?: number;
    functionalSpecializations?: string[];
    [key: string]: unknown;
}
export interface TelegramSource {
    channelId: number;
    channelName: string;
    channelUrl?: string;
    channelUsername?: string;
    messageUrl?: string;
    telegramContact?: string;
}
export interface VacancyRole {
    id: number;
    stableKey: string;
    name: string;
    parentId: number | null;
    sortOrder: number;
    usageCount: number;
    childCount: number;
    isLeaf: boolean;
    path: string;
    descendantIds: number[];
    children?: VacancyRole[];
}
export interface VacancyRoleCatalog {
    roles: VacancyRole[];
    tree: VacancyRole[];
    roots: VacancyRole[];
    leaves: VacancyRole[];
}
export interface Vacancy {
    id: number;
    companyName: string;
    entityType: EntityType;
    realCompanyName: string;
    title: string;
    salaryFrom: number | null;
    salaryTo: number | null;
    role: string;
    roles: VacancyRole[];
    level: string;
    relevance: number | null;
    favorite: boolean;
    hidden: boolean;
    archived: boolean;
    publishedAt: string | null;
    loadedAt: string | null;
    createdAt: string;
    applicationId: number | null;
    applicationStatus: string;
    telegramSources: TelegramSource[];
}
export interface VacancyDetail extends Vacancy {
    publicationSources?: {candidateId:number;url:string;updatedAt:string}[];
    revision: string;
    url: string;
    descriptionMd: string;
    coverLetterSubject: string;
    coverLetterMd: string;
    coverLetterScope: 'shared' | 'personal';
    lastCheckedAt?: string;
    updatedAt?: string;
    employerGuesses?: Record<string, unknown>[];
}
export interface VacancyList {
    vacancies: Vacancy[];
    roleTree?: VacancyRole[];
    entities: Entity[];
    applicationSummary: {
        companyName: string;
        vacancyId: number | null;
    }[];
}
export interface VacancyDeletionResult {
    ok: boolean;
    archived: boolean;
    companyName: string;
    preservedApplication?: boolean;
    preservedTask?: boolean;
}
export interface VacancyDetailResponse {
    vacancy: VacancyDetail;
    entities: Entity[];
}
export interface Task {
    id: number;
    companyName: string;
    vacancyId: number | null;
    vacancyTitle: string;
    type: string;
    note: string;
    dueAt: string;
    completed: boolean;
    googleEventId?: string;
    googleEventHtmlLink?: string;
    googleSyncStatus?: string;
    googleSyncError?: string;
}
export interface TaskList {
    tasks: Task[];
    entities: Entity[];
}
export interface ApplicationCreationContext {
    creationContext: string;
}
export interface ApplicationCreationResult extends ApplicationsResponse {
    createdApplicationId: number;
    replayed: boolean;
}
export interface ApplicationRecord {
    id: number;
    revision: string;
    companyName: string;
    vacancyId: number | null;
    vacancyTitle: string;
    status: string;
    comment: string;
    recruiterName: string;
    recruiterPhone: string;
    recruiterEmail: string;
    recruiterContactType: string;
    applicationSources: string[];
    history: {
        fromStatus: string;
        toStatus: string;
        changedAt: string;
    }[];
}
export interface ApplicationsResponse {
    records: ApplicationRecord[];
    entities?: Entity[];
    vacancies?: Vacancy[];
}
export interface Channel {
    id: number;
    name: string;
    url: string;
    username: string;
    lastSuccessfulCheckAt?: string | null;
    comment: string;
    vacancyCount: number;
}
export interface Navigation {
    openCompany: (name: string, type?: EntityType) => boolean | void;
    openVacancy: (id: number, view?: EntityType) => void;
    openChannel: (id: number) => void;
    vacancyDeleted?: (id: number) => void;
}
export const statuses = ['', 'Откликнулся', 'Получил ответ', 'Скрининг', 'Собес 2', 'Собес 3', 'Собес 4', 'Офер', 'Отказ'];
export const levels = ['Senior', 'Middle', 'Lead', 'Head'];
export const sourceLabels: Record<string, string> = { hh: 'HH.ru', site: 'Сайт', habr: 'Хабр', email: 'Почта', telegram: 'Телеграм', hcm: 'HCM-система' };

export interface VacancyRelevanceResult {vacancyId:number;relevance:number;model:string;}
