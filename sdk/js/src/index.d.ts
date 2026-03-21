export interface Tenant { name: string; isolation: string; policy: string; }
export interface AgentConfig { name: string; tenant: string; model: string; system_prompt: string; tools: string[]; max_turns: number; max_history?: number; rate_limit?: number; }
export interface ChatResponse { agent: string; content: string; tools_used: string[] | null; }
export interface Memory { ID: string; Agent: string; Tenant: string; Key: string; Content: string; CreatedAt: string; UpdatedAt: string; }
export interface Workflow { ID: string; Name: string; Description: string; Tenant: string; Steps: Step[]; }
export interface Step { ID: string; Name: string; Type: string; Config: Record<string, string>; }
export interface WorkflowRun { ID: string; WorkflowID: string; Status: string; CurrentStep: string; StartedAt: string; CompletedAt: string; Error: string; }
export interface Job { ID: string; Name: string; Tenant: string; Agent: string; Message: string; Interval: number; CronExpr: string; Enabled: boolean; }
export interface KnowledgeDoc { id: string; title: string; tags: string; source_url?: string; }
export interface PolicyRule { name: string; tenant: string; action: string; tool: string; agent: string; decision_str: string; priority: number; }
export interface Approval { id: string; tenant: string; agent: string; user: string; tool: string; status: string; }

export declare class CyntrClient {
    constructor(baseUrl?: string, apiKey?: string | null);
    health(): Promise<Record<string, any>>;
    version(): Promise<{ version: string }>;
    listTenants(): Promise<Tenant[]>;
    getTenant(tenant: string): Promise<Tenant>;
    createTenant(name: string, isolation?: string, policy?: string): Promise<{ status: string; name: string }>;
    deleteTenant(tenant: string): Promise<{ status: string }>;
    listAgents(tenant: string): Promise<string[]>;
    createAgent(tenant: string, config: Partial<AgentConfig>): Promise<{ status: string; agent: string }>;
    getAgent(tenant: string, name: string): Promise<AgentConfig>;
    updateAgent(tenant: string, name: string, config: Partial<AgentConfig>): Promise<{ status: string }>;
    deleteAgent(tenant: string, name: string): Promise<{ status: string }>;
    chat(tenant: string, agent: string, message: string): Promise<ChatResponse>;
    listSessions(tenant: string, agent: string): Promise<string[]>;
    getSessionMessages(tenant: string, agent: string, sid: string): Promise<any[]>;
    listMemories(tenant: string, agent: string): Promise<Memory[]>;
    deleteMemory(tenant: string, agent: string, mid: string): Promise<{ status: string }>;
    listSkills(): Promise<any[]>;
    listPolicyRules(): Promise<PolicyRule[]>;
    testPolicy(body: Record<string, string>): Promise<any>;
    listWorkflows(): Promise<string[]>;
    getWorkflow(id: string): Promise<Workflow>;
    registerWorkflow(def: any): Promise<{ workflow_id: string }>;
    runWorkflow(id: string): Promise<{ run_id: string }>;
    listWorkflowRuns(): Promise<WorkflowRun[]>;
    listSchedules(): Promise<Job[]>;
    addSchedule(body: Record<string, any>): Promise<{ status: string; id: string }>;
    queryAudit(params?: Record<string, string>): Promise<any[]>;
    listPeers(): Promise<any[]>;
    listApprovals(): Promise<Approval[]>;
    approve(id: string, decidedBy?: string): Promise<{ status: string }>;
    deny(id: string, decidedBy?: string): Promise<{ status: string }>;
    listChannels(): Promise<any[]>;
    listKnowledge(): Promise<KnowledgeDoc[]>;
    ingestKnowledge(title: string, content: string, tags?: string): Promise<{ status: string; id: string }>;
    deleteKnowledge(id: string): Promise<{ status: string }>;
    chatStream(tenant: string, agent: string, message: string): EventSource;
}
