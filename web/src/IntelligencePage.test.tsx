import axe from "axe-core";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IntelligencePage } from "./IntelligencePage";
import { setCSRFToken } from "./api/client";

const response=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});

describe("IntelligencePage",()=>{
  const requests:Request[]=[];
  beforeEach(()=>{setCSRFToken("csrf-token-with-at-least-thirty-two-characters");vi.stubGlobal("fetch",vi.fn(async(input:RequestInfo|URL)=>{const request=input instanceof Request?input:new Request(input);requests.push(request.clone());const path=new URL(request.url).pathname;
    if(path==="/api/v1/projects")return response({items:[{id:"project-one",provider:"local",repository:"owner/repo",default_branch:"main",local_remote_name:"fixture.git",enabled:true,created_at:"2026-07-20T22:00:00Z",updated_at:"2026-07-20T22:00:00Z"}]});
    if(path.endsWith("/intelligence/status"))return response({project_id:"project-one",latest_revision:"abc123",latest_run_id:"indexrun_fixture",state:"complete",files:2,languages:["go"],failures:0,storage_bytes:120,parser_ids:["controller-syntax-v1"],fresh_at:"2026-07-20T22:00:00Z",indexing_enabled:true,retention_days:30,cache_quota_bytes:536870912});
    if(path.endsWith("/context-manifests"))return response({items:[]});if(path.endsWith("/baselines"))return response({items:[]});if(path.endsWith("/differentials"))return response({items:[]});if(path.endsWith("/test-impacts"))return response({items:[]});if(path.endsWith("/caches"))return response({items:[]});
    if(path.endsWith("/intelligence/query"))return response({project_id:"project-one",revision:"abc123",term:"Target",symbols:[{id:"symbol_one",name:"Target",kind:"function",path:"target.go",start_line:2,end_line:2,confidence:100}],relations:[],partial:false,failures:0});
    if(path.endsWith("/intelligence/actions/refresh"))return response({run:{id:"indexrun_next",project_id:"project-one",repository:"owner/repo",revision:"def456",parser_id:"controller-syntax-v1",state:"complete",files:2,parsed:1,reused:1,failures:0,bytes:120,started_at:"2026-07-20T22:01:00Z",completed_at:"2026-07-20T22:01:00Z"},source_excluded:0},201);
    return response({error:{code:"unmocked",message:`${request.method} ${path}`}},404)}))});
  afterEach(()=>{cleanup();requests.length=0;vi.unstubAllGlobals()});
  it("queries evidence and refreshes only through the trusted snapshot route accessibly",async()=>{const {container}=render(<IntelligencePage/>);expect(await screen.findByText("abc123",{exact:false})).toBeInTheDocument();fireEvent.change(screen.getByLabelText("Graph query"),{target:{value:"Target"}});fireEvent.click(screen.getByRole("button",{name:"Query evidence"}));expect(await screen.findByText("target.go:2")).toBeInTheDocument();fireEvent.click(screen.getByRole("button",{name:"Refresh index"}));expect(await screen.findByText(/1 parsed, 1 reused/)).toBeInTheDocument();const refresh=requests.find((request)=>new URL(request.url).pathname.endsWith("/intelligence/actions/refresh"));expect(refresh?.method).toBe("POST");expect(refresh?.headers.get("X-CSRF-Token")).toBe("csrf-token-with-at-least-thirty-two-characters");expect(await refresh?.clone().text()).toBe("");await waitFor(async()=>expect((await axe.run(container,{runOnly:{type:"tag",values:["wcag2a","wcag2aa"]}})).violations).toEqual([]))});
});
