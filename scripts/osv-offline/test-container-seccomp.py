"""Offline OCI shape and native socket_filter equivalence controls.

No Docker, libseccomp, process launch, Go, network or syscall probe occurs.
Only socket_filter's isolated pure generator is compiled from trusted run.py;
its classic BPF is interpreted as data. The JSON evaluator models documented
MASKED_EQ rule semantics, not a installed/generated kernel filter.

This policy deliberately replaces Docker's default with the original bwrap
native default-ALLOW contract. Docker-default extra restrictions are absent;
the launcher separately owns caps/UID/NNP/resources/mounts/closed FDs. OCI has
no bad-architecture-action field: libseccomp's KILL_THREAD differs from the
generator's KILL_PROCESS. Native-host admission and actual pinned-runtime
compat/x32 child probes remain mandatory. clone3 EPERM (not ENOSYS fallback)
may refuse a libc thread path; Go's ordinary clone flag set is admitted.

Explicit security correction: original numbers 426/427/428 meant enter,
register/open_tree, not the io_uring trio. Setup 425 was omitted. This policy
retains all 29 original denials and adds unconditional setup denial (30 total),
including SQPOLL; denying enter alone is insufficient. The legacy generator
may remain uncorrected or receive the same separate integration correction.

Primary sources read for this preparation:
https://docs.docker.com/engine/security/seccomp/
https://raw.githubusercontent.com/opencontainers/runtime-spec/v1.2.1/config-linux.md
https://raw.githubusercontent.com/seccomp/libseccomp/v2.5.5/src/gen_bpf.c
https://raw.githubusercontent.com/seccomp/libseccomp/v2.5.5/src/db.c
https://raw.githubusercontent.com/seccomp/libseccomp/v2.5.5/include/seccomp.h.in
https://raw.githubusercontent.com/opencontainers/runc/v1.2.6/libcontainer/seccomp/seccomp_linux.go
https://raw.githubusercontent.com/torvalds/linux/v6.12/arch/x86/entry/syscalls/syscall_64.tbl
https://raw.githubusercontent.com/torvalds/linux/v6.12/include/uapi/linux/sched.h
"""
import ast
import copy
import errno
import json
from pathlib import Path
import struct
import unittest

HERE=Path(__file__).resolve().parent
ARCH=0xc000003e
ALLOW=0x7fff0000
EPERM=0x00050000|errno.EPERM
KILL_PROCESS=0x80000000
NAMESPACE_BITS=(0x00020000,0x02000000,0x04000000,0x08000000,0x10000000,0x20000000,0x40000000)
# Native x86_64 names/numbers from Linux v6.12's syscall table; no compat map.
DENIED={41:'socket',42:'connect',43:'accept',44:'sendto',45:'recvfrom',46:'sendmsg',47:'recvmsg',
    48:'shutdown',49:'bind',50:'listen',51:'getsockname',52:'getpeername',53:'socketpair',
    54:'setsockopt',55:'getsockopt',101:'ptrace',155:'pivot_root',165:'mount',166:'umount2',
    272:'unshare',288:'accept4',299:'recvmmsg',307:'sendmmsg',308:'setns',321:'bpf',
    425:'io_uring_setup',426:'io_uring_enter',427:'io_uring_register',428:'open_tree',435:'clone3'}
ORIGINAL_DENIED={nr:name for nr,name in DENIED.items() if nr!=425}

def strict_object(pairs):
    result={}
    for key,value in pairs:
        if key in result: raise ValueError('duplicate JSON key')
        result[key]=value
    return result

def load_policy():
    raw=(HERE/'container-seccomp.json').read_bytes()
    if len(raw)>16384: raise ValueError('policy size bound')
    return json.loads(raw,object_pairs_hook=strict_object)

def validate(policy):
    """Reviewed strict subset of standard OCI seccomp, no Docker extensions."""
    if not isinstance(policy,dict) or set(policy)!={'defaultAction','architectures','syscalls'}:
        raise ValueError('nonstandard or unexpected top-level field')
    if policy['defaultAction']!='SCMP_ACT_ALLOW' or policy['architectures']!=['SCMP_ARCH_X86_64']:
        raise ValueError('native default/architecture contract changed')
    rules=policy['syscalls']
    if not isinstance(rules,list) or len(rules)!=8: raise ValueError('exact bounded rule count')
    unconditional=[]; clone_bits=[]
    for rule in rules:
        if not isinstance(rule,dict) or set(rule) not in ({'names','action','errnoRet'},{'names','action','errnoRet','args'}):
            raise ValueError('nonstandard or unexpected rule field')
        if rule['action']!='SCMP_ACT_ERRNO' or type(rule['errnoRet']) is not int or rule['errnoRet']!=errno.EPERM:
            raise ValueError('denial must be EPERM')
        names=rule['names']
        if not isinstance(names,list) or not names or any(not isinstance(n,str) for n in names) or len(names)!=len(set(names)):
            raise ValueError('nonempty unique syscall names required')
        if 'args' not in rule:
            unconditional.extend(names); continue
        args=rule['args']
        if names!=['clone'] or not isinstance(args,list) or len(args)!=1:
            raise ValueError('only exact clone argument rule allowed')
        arg=args[0]
        if not isinstance(arg,dict) or set(arg)!={'index','value','valueTwo','op'}:
            raise ValueError('standard explicit masked comparison required')
        if any(type(arg[key]) is not int for key in ('index','value','valueTwo')) or arg['index']!=0 or arg['op']!='SCMP_CMP_MASKED_EQ':
            raise ValueError('x86_64 clone flags must use argument zero')
        if arg['value'] not in NAMESPACE_BITS or arg['valueTwo']!=arg['value']:
            raise ValueError('one namespace bit must trigger each denial')
        clone_bits.append(arg['value'])
    if len(unconditional)!=30 or set(unconditional)!=set(DENIED.values()):
        raise ValueError('exact native denial set changed')
    if sorted(clone_bits)!=sorted(NAMESPACE_BITS): raise ValueError('namespace OR coverage changed')

def json_action(policy,name,args=(0,0,0,0,0,0)):
    for rule in policy['syscalls']:
        if name not in rule['names']: continue
        matches=True
        for arg in rule.get('args',[]):
            if arg['op']!='SCMP_CMP_MASKED_EQ': raise ValueError('unsupported test operator')
            matches &= args[arg['index']] & arg['value']==arg['valueTwo']
        if matches: return EPERM
    return ALLOW

def original_filter():
    tree=ast.parse((HERE/'run.py').read_text())
    functions=[n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='socket_filter']
    if len(functions)!=1: raise ValueError('exact pure original generator missing')
    function=functions[0]
    # Source extraction never imports run.py or executes its entry/bootstrap.
    allowed_names={'rows','denied','nr','row','sorted','struct','errno'}
    for node in ast.walk(function):
        if isinstance(node,ast.Name) and node.id not in allowed_names: raise ValueError('generator dependency changed')
        if isinstance(node,(ast.Import,ast.ImportFrom,ast.Global,ast.Nonlocal)): raise ValueError('generator is no longer isolated')
    namespace={'__builtins__':{'sorted':sorted},'struct':struct,'errno':errno}
    exec(compile(ast.Module(body=[function],type_ignores=[]),str(HERE/'run.py'),'exec'),namespace)
    raw=namespace['socket_filter']()
    if len(raw)>1024 or len(raw)%8: raise ValueError('original filter byte bound')
    return [struct.unpack('<HBBI',raw[i:i+8]) for i in range(0,len(raw),8)]

def bpf_action(program,nr,args=(0,0,0,0,0,0),arch=ARCH):
    data=struct.pack('<IIQ6Q',nr&0xffffffff,arch,0,*args)
    pc=0; accumulator=0
    for _ in range(128):
        code,jt,jf,value=program[pc]
        if code==0x20: accumulator=struct.unpack_from('<I',data,value)[0]; pc+=1
        elif code==0x15: pc+=1+(jt if accumulator==value else jf)
        elif code==0x45: pc+=1+(jt if accumulator&value else jf)
        elif code==0x06: return value
        else: raise ValueError('unsupported original instruction')
    raise ValueError('original filter does not terminate')

class ContainerSeccompTests(unittest.TestCase):
    def setUp(self):
        self.policy=load_policy(); validate(self.policy); self.program=original_filter()

    def test_standard_oci_shape_and_exact_native_architecture(self):
        self.assertEqual(set(self.policy),{'defaultAction','architectures','syscalls'})
        self.assertEqual(self.policy['architectures'],['SCMP_ARCH_X86_64'])
        self.assertNotIn('defaultErrnoRet',self.policy)
        self.assertNotIn('archMap',self.policy)
        self.assertEqual(len(self.policy['syscalls']),8)

    def test_all_29_original_native_denials_are_eperm_for_arbitrary_arguments(self):
        for nr,name in ORIGINAL_DENIED.items():
            for args in ((0,)*6,((1<<64)-1,)*6,(1,2,3,4,5,6)):
                with self.subTest(nr=nr,name=name,args=args):
                    self.assertEqual(bpf_action(self.program,nr,args),EPERM)
                    self.assertEqual(json_action(self.policy,name,args),EPERM)

    def test_setup_425_denied_independently_including_sqpoll_and_open_tree_retained(self):
        for args in ((0,0,0,0,0,0),(4096,2,0,0,0,0),((1<<64)-1,)*6):
            self.assertEqual(json_action(self.policy,'io_uring_setup',args),EPERM)
        self.assertEqual(json_action(self.policy,'open_tree'),EPERM)
        self.assertEqual(bpf_action(self.program,428),EPERM)
        # Do not claim the unedited legacy generator is fixed: integration may
        # separately correct 425, while every inherited denial stays exact.
        self.assertIn(bpf_action(self.program,425),(ALLOW,EPERM))
        missing_setup=copy.deepcopy(self.policy)
        missing_setup['syscalls'][0]['names'].remove('io_uring_setup')
        self.assertEqual(json_action(missing_setup,'io_uring_setup'),ALLOW)
        with self.assertRaises(ValueError): validate(missing_setup)

    def test_every_socket_family_and_socketpair_is_unconditionally_refused(self):
        for family in (0,1,2,10,16,17,38,40,(1<<32)-1):
            args=(family,1,0,0,0,0)
            for name in ('socket','socketpair','connect','sendmsg','recvmsg','setsockopt','getsockopt'):
                with self.subTest(family=family,name=name): self.assertEqual(json_action(self.policy,name,args),EPERM)

    def test_namespace_clone_or_is_equivalent_for_all_128_combinations(self):
        ordinary=0x100|0x200|0x400|0x800|0x10000|0x40000|0x80000|0x100000|0x200000
        for selected in range(128):
            mask=sum(bit for index,bit in enumerate(NAMESPACE_BITS) if selected&(1<<index))
            for extra in (0,ordinary,1<<63):
                args=(mask|extra,0,0,0,0,0)
                with self.subTest(selected=selected,extra=extra):
                    expected=EPERM if selected else ALLOW
                    self.assertEqual(json_action(self.policy,'clone',args),expected)
                    self.assertEqual(bpf_action(self.program,56,args),expected)

    def test_go_ordinary_threads_fork_vfork_and_exec_remain_allowed(self):
        # Go runtime thread clone: VM|FS|FILES|SIGHAND|SYSVSEM|THREAD.
        flags=0x100|0x200|0x400|0x800|0x40000|0x10000
        self.assertEqual(json_action(self.policy,'clone',(flags,0,0,0,0,0)),ALLOW)
        self.assertEqual(bpf_action(self.program,56,(flags,0,0,0,0,0)),ALLOW)
        for nr,name in ((0,'read'),(1,'write'),(57,'fork'),(58,'vfork'),(59,'execve'),(202,'futex'),
                (231,'exit_group'),(257,'openat'),(318,'getrandom'),(322,'execveat'),(999,'future_native_syscall')):
            with self.subTest(name=name):
                self.assertEqual(json_action(self.policy,name),ALLOW)
                self.assertEqual(bpf_action(self.program,nr),ALLOW)

    def test_original_wrong_abi_kills_and_json_adds_no_compat_support(self):
        # This is the original BPF oracle, not proof of OCI compilation/loading.
        for arch in (0x40000003,0xc00000b7,0):
            self.assertEqual(bpf_action(self.program,41,arch=arch),KILL_PROCESS)
        for nr in (0x40000000,0x40000029,0x40000038):
            self.assertEqual(bpf_action(self.program,nr),KILL_PROCESS)
        self.assertEqual(self.policy['architectures'],['SCMP_ARCH_X86_64'])

    def test_adverse_schema_and_policy_mutations_fail_offline_gate(self):
        mutations=[]
        def changed(update):
            policy=copy.deepcopy(self.policy); update(policy); mutations.append(policy)
        changed(lambda p:p.update(defaultAction='SCMP_ACT_ERRNO'))
        changed(lambda p:p.update(defaultErrnoRet=1))
        changed(lambda p:p.update(archMap=[]))
        changed(lambda p:p['architectures'].append('SCMP_ARCH_X32'))
        changed(lambda p:p['architectures'].append('SCMP_ARCH_X86'))
        changed(lambda p:p['syscalls'][0].update(includes={'caps':['CAP_SYS_ADMIN']}))
        changed(lambda p:p['syscalls'][0].update(errnoRet=38))
        changed(lambda p:p['syscalls'][0].update(errnoRet=True))
        changed(lambda p:p['syscalls'][0]['names'].remove('socketpair'))
        changed(lambda p:p['syscalls'][0]['names'].append('clone'))
        changed(lambda p:p['syscalls'][1]['args'][0].update(index=1))
        changed(lambda p:p['syscalls'][1]['args'][0].update(value=0x7e020000,valueTwo=0x7e020000))
        changed(lambda p:p['syscalls'][1]['args'][0].update(valueTwo=0))
        changed(lambda p:p['syscalls'].pop())
        for index,policy in enumerate(mutations):
            with self.subTest(index=index),self.assertRaises(ValueError): validate(policy)

    def test_masked_eq_all_bits_together_would_allow_single_namespace(self):
        broken=copy.deepcopy(self.policy)
        broken['syscalls']=broken['syscalls'][:2]
        broken['syscalls'][1]['args'][0].update(value=0x7e020000,valueTwo=0x7e020000)
        # Meaningful adverse semantic control: the common single-mask mistake
        # fails to express OR and permits each isolated namespace request.
        for bit in NAMESPACE_BITS:
            args=(bit,0,0,0,0,0)
            self.assertEqual(json_action(broken,'clone',args),ALLOW)
            self.assertEqual(json_action(self.policy,'clone',args),EPERM)

    def test_duplicate_json_key_refused(self):
        with self.assertRaises(ValueError): json.loads('{"defaultAction":1,"defaultAction":2}',object_pairs_hook=strict_object)

if __name__=='__main__': unittest.main()
