package main

import (
	"fmt"
	"log"
	"flag"
	"os"
	"bufio"
	"encoding/json"
	"path/filepath"
	"github.com/trivago/grok"
)

type Value interface {
	String() string
	Set(string) error
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func handleArguments(server_logs, agent_logs []string, output_file, output_ts  string) ([]*os.File, error){
	var fileHandlers []*os.File

	pathOK := true
	for _, n := range append(server_logs, agent_logs...) {
		p := string(n)
		f, err := os.Stat(string(p))
		if err == nil {
		}

		if os.IsNotExist(err) {
			log.Println(err.Error())
			pathOK = false
		}

		if f.IsDir() {
			log.Printf("%s should be a file, not a directory.\n", f.Name())
			pathOK = false
		}

		fr, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		fileHandlers = append(fileHandlers, fr)
	}
	if !pathOK {
		return nil, fmt.Errorf("Invalid path(s) provided.")
	}

	dir, err := filepath.Abs(filepath.Dir(output_file))
	if err != nil {
		return nil, err
	} else {
		_, err := os.Stat(dir)
		if os.IsNotExist(err) {
			log.Fatal(err)
			return nil, err
		}
	}

	return fileHandlers, nil
}

func read_and_match(g *grok.Grok, fr *os.File, p map[string]string, result map[string]map[string]string) {
	defer fr.Close()

	scanner := bufio.NewScanner(fr)
	maxCapacity := 512 * 1024  // default max size 64*1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	lines := 0
	for scanner.Scan() {
		lines += 1
		if lines % 50000 == 0 {
			log.Printf("read %d lines\n", lines)
		}
		// fmt.Println(scanner.Text())
		for k, _ := range p {
			// fmt.Println(scanner.Text())
			// fmt.Println(k)
			values, err := g.ParseString(fmt.Sprintf("%%{%s}", k), scanner.Text())
			if err != nil {
				log.Fatal(err)
			 }
		
			if len(values) == 0 {
				// log.Println("no match for this pattern.")
				continue
			}
		
			if _, ok := values["req_id"]; !ok {
				log.Fatal("no req-id matched.")
			 }

			req_id := values["req_id"]
			if _, ok := result[req_id]; !ok {
				result[req_id] = map[string]string{}
			}

			 for k, v := range values {
				//  log.Printf("%+25s: %s\n", k, v)
				result[req_id][k] = v

			 }

			 break
			//  log.Println()
		}
		
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error happens at line %d\n", lines)
		log.Fatal(err)
	} else {
		log.Printf("Read from file %s, lines: %d\n", fr.Name(), lines)
	}
}

func main() {

	var server_logs arrayFlags
	var agent_logs arrayFlags
	var output_file string
	var output_ts string

	flag.Var(&server_logs, "server-log", "Neutron server log path: /path/to/server.log, e.g: /var/log/neutron/server.log")
	flag.Var(&agent_logs, "agent-log", "F5-OpenStack-Agent log path: /path/to/f5-openstack-agent.log, e.g: /var/log/neutron/f5-openstack-agent.log")
	flag.StringVar(&output_file, "output-filepath", "./result.json", "Output the result to file, e.g: /path/to/result.json")
	flag.StringVar(&output_ts, "output-ts", "./result.json", "Output the result to f5-telemetry-analytics. e.g: http://1.1.1.1:200002")

	flag.Parse()

	fmt.Println(server_logs)
	fmt.Println(agent_logs)
	fmt.Println(output_file)
	fmt.Println(output_ts)

	fileHandlers, err := handleArguments(server_logs, agent_logs, output_file, output_file)
	if err != nil {
		log.Fatal(err)
	}


	pBasicFields := map[string]string{
		"UUID": `[a-z0-9]{8}-([a-z0-9]{4}-){3}[a-z0-9]{12}`,    	// 6245c77d-5017-4657-b35b-7ab1d247112b
		"REQID": `req-%{UUID}`,										// req-8cadad28-8315-45ca-818c-6a229dfb73e1
		"DATETIME": `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}.\d{3}`,	// 2020-09-27 19:22:54.486
		"MD5": `[0-9a-z]{32}`, 										// 62c38230485b4794a8eedece5dac9192
		"JSON": `\{.*\}`,											// {u'bandwidth_limit_rule': {u'max_kbps': 102400, u'direction': u'egress', u'max_burst_kbps': 102400}}
		"LBTYPE": `(LoadBalancer|Listener|Pool|Member|HealthMonitor)`,
		"LBTYPESTR": `(loadbalancer|listener|pool|member|health_monitor)`,
		"ACTION": `(create|update|delete)`,
	}

	pLBaaSv2 := map[string]string{

		// 2020-09-27 19:22:54.485 68316 DEBUG neutron.api.v2.base 
		// [req-8cadad28-8315-45ca-818c-6a229dfb73e1 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] Request body: 
		// {u'bandwidth_limit_rule': {u'max_kbps': 102400, u'direction': u'egress', u'max_burst_kbps': 102400}} 
		// prepare_request_body /usr/lib/python2.7/site-packages/neutron/api/v2/base.py:713
		"neutron_api_v2_base": `%{DATETIME:neutron_api_time} .* neutron.api.v2.base \[%{REQID:req_id} .*\] ` +
							   `Request body: %{JSON:request_body} prepare_request_body .*$`,

		// 05neu-core/server.log-1005:2020-10-05 10:20:17.251 117825 DEBUG f5lbaasdriver.v2.bigip.driver_v2 
		// [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] 
		// f5lbaasdriver.v2.bigip.driver_v2.LoadBalancerManager method create called with arguments (<neutron_lib.context.Context object at 0x284cb250>, 
		// <neutron_lbaas.services.loadbalancer.data_models.LoadBalancer object at 0xdb44250>) {} 
		// wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66
		"call_f5driver": 
			`%{DATETIME:call_f5driver_time} .* f5lbaasdriver.v2.bigip.driver_v2 \[%{REQID:req_id} .*\] ` +
			`f5lbaasdriver.v2.bigip.driver_v2.%{LBTYPE:object_type}Manager method %{ACTION:operation_type} called with .*$`,
		
		// 2020-10-05 10:20:21.924 117825 DEBUG f5lbaasdriver.v2.bigip.agent_scheduler 
		// [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] 
		// Loadbalancer e2d277f7-eca2-46a4-bf2c-655856fd8733 is scheduled to lbaas agent dc55e196-319a-4c82-b262-344f45415131 schedule 
		// /usr/lib/python2.7/site-packages/f5lbaasdriver/v2/bigip/agent_scheduler.py:306
		// "agent_scheduled": 

		// 2020-10-05 10:20:27.176 117825 DEBUG f5lbaasdriver.v2.bigip.agent_rpc [req-92db71fb-8
		// 513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default]
		// f5lbaasdriver.v2.bigip.agent_rpc.LBaaSv2AgentRPC method create_loadbalancer called with arguments (<neutron_lib.
		// context.Context object at 0x284cb250>, {'availability_zone_hints': [], 'description': '', 'admin_state_up': True
		// , 'tenant_id': '62c38230485b4794a8eedece5dac9192', 'provisioning_status': 'PENDING_CREATE', 'listeners': [], 'vi
		// p_subnet_id': 'd79ef712-c1e3-4860-9343-d1702b9976aa', 'vip_address': '10.230.44.15', 'vip_port_id': '5bcbe2d7-99
		// 4f-40de-87ab-07aa632f0133', 'provider': None, 'pools': [], 'id': 'e2d277f7-eca2-46a4-bf2c-655856fd8733', 'operat
		// ing_status': 'OFFLINE', 'name': 'JL-B01-POD1-CORE-LB-7'}, {'subnets': ...
		// : 'd79ef712-c1e3-4860-9343-d1702b9976aa', 'vip_address': '10.230.44.15', 'vip_port_id': '5bcbe2d7-994f-40de-87ab
		// -07aa632f0133', 'provider': None, 'pools': [], 'id': 'e2d277f7-eca2-46a4-bf2c-655856fd8733', 'operating_status':
		// 'OFFLINE', 'name': 'JL-B01-POD1-CORE-LB-7'}}, u'POD1_CORE3') {} wrapper /usr/lib/python2.7/site-packages/oslo_l
		// og/helpers.py:66
		"rpc_f5agent": 
			`%{DATETIME:rpc_f5agent_time} .* f5lbaasdriver.v2.bigip.agent_rpc \[%{REQID:req_id} .*\] ` +
			`f5lbaasdriver.v2.bigip.agent_rpc.LBaaSv2AgentRPC method %{ACTION}_%{LBTYPESTR} called with arguments ` +
			`.*? 'id': '%{UUID:object_id}'.*`,

		// 2020-10-05 10:19:16.315 295263 DEBUG f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager 
		// [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] 
		// f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager.LbaasAgentManager method create_loadbalancer called with arguments
		// ...
		// 7'}} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66
		"call_f5agent": 
			`%{DATETIME:call_f5agent_time} .* f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager \[%{REQID:req_id} .*\] ` +
			`f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager.LbaasAgentManager method %{ACTION}_%{LBTYPESTR} ` +
			`called with arguments .*`,

		// 2020-10-05 10:19:16.317 295263 DEBUG root [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 
		// 62c38230485b4794a8eedece5dac9192 - - -] get WITH uri: https://10.216.177.8:443/mgmt/tm/sys/folder/~CORE_62c38230485b4794a8eedece5dac9192 AND 
		// suffix:  AND kwargs: {} wrapper /usr/lib/python2.7/site-packages/icontrol/session.py:257
		"rest_call_bigip": 
			`%{DATETIME:call_bigip_time} .* \[%{REQID:req_id} .*\] get WITH uri: .*icontrol/session.py.*`,

		// 2020-10-05 10:19:18.411 295263 DEBUG f5_openstack_agent.lbaasv2.drivers.bigip.plugin_rpc 
		// [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] 
		// f5_openstack_agent.lbaasv2.drivers.bigip.plugin_rpc.LBaaSv2PluginRPC method update_loadbalancer_status called with arguments 
		// (u'e2d277f7-eca2-46a4-bf2c-655856fd8733', 'ACTIVE', 'ONLINE', u'JL-B01-POD1-CORE-LB-7') {} wrapper 
		// /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66
		"update_loadbalancer_status": 
			`%{DATETIME:update_status_time} .* f5_openstack_agent.lbaasv2.drivers.bigip.plugin_rpc \[%{REQID:req_id} .*\].* ` +
			`method update_loadbalancer_status called with arguments.*`,
		
		// "test_basic_pattern":
		// 	`%{LBTYPE:object_type}`,
	}

	pattern :=map[string]string {}

	for k, v := range pBasicFields {
		pattern[k] = v
	}

	for k, v := range pLBaaSv2 {
		pattern[k] = v
	}

	g, e := grok.New(grok.Config{
		NamedCapturesOnly: true,
		Patterns: pattern,
	})
	if e != nil {
		log.Panic(e)
	}

	result := map[string]map[string]string{}
	for _, f := range(fileHandlers) {
		read_and_match(g, f, pLBaaSv2, result)
	}
	

	// for k, vs := range tests() {
	// 	fmt.Printf("------- %s --------\n", k)
	// 	for _, v := range vs {
	// 		value, err := test_sg(k, v, g)
	// 		debug(value, err)
	// 	}
	// }

	buff, err := json.Marshal(result)
	fmt.Printf("%s\n", buff)
	for _, v := range result {
		fmt.Printf("%s,%s,%s,%s,\"%s\",%s,%s,%s,%s,%s,%s\n",
			v["req_id"],
			v["object_id"],
			v["object_type"],
			v["operation_type"],
			v["request_body"],
			v["neutron_api_time"],
			v["call_f5driver_time"],
			v["rpc_f5agent_time"],
			v["call_f5agent_time"],
			v["call_bigip_time"],
			v["update_status_time"],
		)
	}
}

func debug(values map[string]string, e error) {
	if e != nil {
		log.Println(e.Error())
		return
	 }

	 if len(values) == 0 {
		 log.Println("no match for this pattern.")
		 return
	 }

	 for k, v := range values {
		 log.Printf("%+25s: %s\n", k, v)
	 }
	 log.Println()
}

func test_sg(k string, v string, g *grok.Grok) (map[string]string, error) {
	return g.ParseString(fmt.Sprintf("%%{%s}", k), v)
}

func tests() map[string][]string {
	return map[string][]string{
		"neutron_api_v2_base":[]string{
				// loadbalancer
				`2020-10-05 10:20:15.791 117825 DEBUG neutron.api.v2.base [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] Request body: {u'loadbalancer': {u'vip_subnet_id': u'd79ef712-c1e3-4860-9343-d1702b9976aa', u'provider': u'core', u'name': u'JL-B01-POD1-CORE-LB-7', u'admin_state_up': True}} prepare_request_body /usr/lib/python2.7/site-packages/neutron/api/v2/base.py:713`,	
				// member
				`2020-10-05 14:50:24.795 117812 DEBUG neutron.api.v2.base [req-be08ea84-f721-46da-b24e-6e2c249af84e 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] Request body: {u'member': {u'subnet_id': u'5ee954be-8a76-4e42-b7a9-13a08e5330ce', u'address': u'10.230.3.39', u'protocol_port': 39130, u'weight': 5, u'admin_state_up': True}} prepare_request_body /usr/lib/python2.7/site-packages/neutron/api/v2/base.py:713`,
			},

		"call_f5driver": []string{
				// loadbalancer
				`2020-10-05 10:20:17.251 117825 DEBUG f5lbaasdriver.v2.bigip.driver_v2 [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] f5lbaasdriver.v2.bigip.driver_v2.LoadBalancerManager method create called with arguments (<neutron_lib.context.Context object at 0x284cb250>, <neutron_lbaas.services.loadbalancer.data_models.LoadBalancer object at 0xdb44250>) {} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,
				// member
				`2020-10-05 14:50:28.214 117812 DEBUG f5lbaasdriver.v2.bigip.driver_v2 [req-be08ea84-f721-46da-b24e-6e2c249af84e 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] f5lbaasdriver.v2.bigip.driver_v2.MemberManager method create called with arguments (<neutron_lib.context.Context object at 0x1310cc90>, <neutron_lbaas.services.loadbalancer.data_models.Member object at 0x286ed750>) {} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,
			},

		"rpc_f5agent": []string{
				// loadbalancer
				`2020-10-05 10:20:27.176 117825 DEBUG f5lbaasdriver.v2.bigip.agent_rpc [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] f5lbaasdriver.v2.bigip.agent_rpc.LBaaSv2AgentRPC method create_loadbalancer called with arguments (<neutron_lib.context.Context object at 0x284cb250>, {'availability_zone_hints': [], 'description': '', 'admin_state_up': True, 'tenant_id': '62c38230485b4794a8eedece5dac9192', 'provisioning_status': 'PENDING_CREATE', 'listeners': [], 'vip_subnet_id': 'd79ef712-c1e3-4860-9343-d1702b9976aa', 'vip_address': '10.230.44.15', 'vip_port_id': '5bcbe2d7-994f-40de-87ab-07aa632f0133', 'provider': None, 'pools': [], 'id': 'e2d277f7-eca2-46a4-bf2c-655856fd8733', 'operating_status': 'OFFLINE', 'name': 'JL-B01-POD1-CORE-LB-7'}, {'subnets': {u'd79ef71...OD1_CORE3') {} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,

				// member
				`2020-10-05 14:51:54.445 117812 DEBUG f5lbaasdriver.v2.bigip.agent_rpc [req-be08ea84-f721-46da-b24e-6e2c249af84e 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - default default] f5lbaasdriver.v2.bigip.agent_rpc.LBaaSv2AgentRPC method create_member called with arguments (<neutron_lib.context.Context object at 0x1310cc90>, {'name': '', 'weight': 5, 'provisioning_status': 'PENDING_CREATE', 'subnet_id': '5ee954be-8a76-4e42-b7a9-13a08e5330ce', 'tenant_id': '62c38230485b4794a8eedece5dac9192', 'admin_state_up': True, 'pool_id': '100858a1-8ba9-496c-9cb4-7d1143431ce8', 'address': '10.230.3.39', 'protocol_port': 39130, 'id': '551b7992-273f-4923-94f2-57b12a715c15', 'operating_status': 'OFFLINE'}, {'subne...18273-1f5e-4be2-a263-ce37823a7773', 'operating_status': 'ONLINE', 'name': 'JL-B01-POD1-CORE-LB-1'}}, u'POD1_CORE') {} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,
			},

		"call_f5agent":[]string{
				// loadbalancer
				`2020-10-05 10:19:16.315 295263 DEBUG f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager.LbaasAgentManager method create_loadbalancer called with arguments (<neutron_lib.context.Context object at 0x7351290>,) {u'service': {u'subnets': {u'd79ef712-c1e3-4860-9343-d1702b9976aa': {u'updated_at': u'2020-09-25T05:29:56Z', u'ipv6_ra_mode': None, u'allocation_pools': [{u'start': u'10.230.44.2', u'end': u'10.230.44.30'}], u'host_routes': [], u'revision_number': 1, u'ipv6_address_mode': None, u'id': u'd79ef712-c1e3-4860-9343-d1702b9976aa', u'available_ips': [{u'start': u'10.230.44.3', u'end': u'10.230.44.3'}, {u'start': u'10.230.44.10', u'end': u'10.230.44.12'}, {u'start': u'10.230.44.14', u'end': u'10.230.44.14'}, {u'start': u'10.230...'JL-B01-POD1-CORE-LB-7'}} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,

				// member
				`2020-10-05 12:14:41.917 295263 DEBUG f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager [req-8f058904-e3f8-401b-b637-97cb5b46f7eb 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] f5_openstack_agent.lbaasv2.drivers.bigip.agent_manager.LbaasAgentManager method create_member called with arguments (<neutron_lib.context.Context object at 0x7648a50>,) {u'member': {u'name': u'', u'weight': 5, u'admin_state_up': True, u'subnet_id': u'5ee954be-8a76-4e42-b7a9-13a08e5330ce', u'tenant_id': u'62c38230485b4794a8eedece5dac9192', u'provisioning_status': u'PENDING_CREATE', u'pool_id': u'7aabf08d-70aa-4df8-a26f-fde15893b90f', u'address': u'10.230.3.17', u'protocol_port': 39161, u'id': u'43b2c465-d82d-4a5f-951d-8f30837be3f2', u'operating_status': u'OFFLINE'}, u'service': {u'subnets': {u'5ee954be-8a76-4e42-b7a9-13a08e5330ce': {u'updated_at': ...emetal'}, u'operating_status': u'ONLINE', u'name': u'JL-B01-POD1-CORE-LB-2'}}} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,
			},

		"rest_call_bigip":[]string{
				// loadbalancer
				`2020-10-05 10:19:16.317 295263 DEBUG root [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] get WITH uri: https://10.216.177.8:443/mgmt/tm/sys/folder/~CORE_62c38230485b4794a8eedece5dac9192 AND suffix:  AND kwargs: {} wrapper /usr/lib/python2.7/site-packages/icontrol/session.py:257`,
			},

		"update_loadbalancer_status":[]string{
				// loadbalancer
				`2020-10-05 10:19:18.411 295263 DEBUG f5_openstack_agent.lbaasv2.drivers.bigip.plugin_rpc [req-92db71fb-8513-431b-ac79-5423a749b6d7 009ac6496334436a8eba8daa510ef659 62c38230485b4794a8eedece5dac9192 - - -] f5_openstack_agent.lbaasv2.drivers.bigip.plugin_rpc.LBaaSv2PluginRPC method update_loadbalancer_status called with arguments (u'e2d277f7-eca2-46a4-bf2c-655856fd8733', 'ACTIVE', 'ONLINE', u'JL-B01-POD1-CORE-LB-7') {} wrapper /usr/lib/python2.7/site-packages/oslo_log/helpers.py:66`,
			},

		"test_basic_pattern":[]string{
				// loadbalancer
				`LoadBalancerManager`,
			},
	}
}
