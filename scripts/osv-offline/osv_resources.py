"""Closed owner-approved decimal GB scan policy; no freeform quota authority."""
import os
import re

DEFAULT_MEMORY = 2147483648
SCAN_MEMORY = 3000000000
PAGE_SIZE = 4096
SCAN_KERNEL_MEMORY = SCAN_MEMORY // PAGE_SIZE * PAGE_SIZE  #2999996416; lower by3584
REQUIRED_HOST_PARENT_HEADROOM = SCAN_MEMORY + DEFAULT_MEMORY
PHASE_MEMORY = {
    'prepare-scheduled': DEFAULT_MEMORY, 'prepare-differential': DEFAULT_MEMORY,
    'scan-scheduled': DEFAULT_MEMORY, 'scan-baseline': SCAN_MEMORY, 'scan-candidate': SCAN_MEMORY,
    'report-scheduled': DEFAULT_MEMORY, 'report-differential': DEFAULT_MEMORY,
    'gate-scheduled': DEFAULT_MEMORY, 'gate-differential': DEFAULT_MEMORY,
}
OWNER_APPROVAL_SHA256 = 'a39bcb6cbbefe722afa7ea21916d786c7e6073f1f5122ab86cabb921a0e783b4'
ACTUAL_PARENT_STARTUP_GUARD_SHA256 = 'ba58180c97506aad0463280f5a75cd2c79fc99bc021bf6b68a718a5866bb5a86'

class ResourceRefusal(RuntimeError): pass

def requested_memory(phase=None):
    if phase is not None and (type(phase) is not str or phase not in PHASE_MEMORY):
        raise ResourceRefusal('unknown resource phase')
    return DEFAULT_MEMORY if phase is None else PHASE_MEMORY[phase]

def require_page_size():
    if os.sysconf('SC_PAGE_SIZE') != PAGE_SIZE:raise ResourceRefusal('unsupported memory page size')
    return PAGE_SIZE

def limits_for_phase(phase=None):
    memory=requested_memory(phase)
    return {'memory.max':str(SCAN_KERNEL_MEMORY if memory==SCAN_MEMORY else memory),
        'memory.swap.max':'0','pids.max':'256'}

def authorize_owner(phase, owner_hash):
    limits=limits_for_phase(phase)
    if requested_memory(phase) != SCAN_MEMORY:
        if owner_hash is not None:raise ResourceRefusal('default2GiB phase has no owner expansion binding')
        return limits
    if OWNER_APPROVAL_SHA256 is None:raise ResourceRefusal('owner3GB phase authority is unbound')
    if owner_hash != OWNER_APPROVAL_SHA256:raise ResourceRefusal('owner3GB phase authority differs')
    if ACTUAL_PARENT_STARTUP_GUARD_SHA256 is None:raise ResourceRefusal('actual-parent startup source guard is unbound')
    return limits

def authorize_phase(phase, authority):
    limits=limits_for_phase(phase)
    if requested_memory(phase)==DEFAULT_MEMORY:
        if authority is not None:raise ResourceRefusal('default2GiB phase has no expansion authority')
        return limits
    authorize_owner(phase,OWNER_APPROVAL_SHA256)
    required={'phase','memory_bytes','owner_approval_sha256','startup_guard_sha256',
        'actual_headroom_receipt_sha256','core_contract_sha256'}
    if (type(authority) is not dict or set(authority)!=required or authority['phase']!=phase or
        type(authority['memory_bytes']) is not int or authority['memory_bytes']!=SCAN_MEMORY or
        authority['owner_approval_sha256']!=OWNER_APPROVAL_SHA256 or
        authority['startup_guard_sha256']!=ACTUAL_PARENT_STARTUP_GUARD_SHA256 or
        any(type(authority[key]) is not str or not re.fullmatch('[0-9a-f]{64}',authority[key]) for key in
            ('owner_approval_sha256','startup_guard_sha256','actual_headroom_receipt_sha256','core_contract_sha256'))):
        raise ResourceRefusal('phase resource authority binding differs')
    return limits

def validate_witness(phase, authority, witness):
    expected=authorize_phase(phase,authority);require_page_size()
    if (witness.get('resource_phase')!=phase or witness.get('resource_authority')!=authority or
        witness.get('requested_memory_bytes')!=requested_memory(phase) or witness.get('memory_page_size')!=PAGE_SIZE or
        witness.get('kernel_limits')!=expected or witness.get('child',{}).get('memory')!=expected['memory.max']):
        raise ResourceRefusal('phase worker/child quota binding differs')
    return expected

def source_proposal():
    return {'status':'OWNER_APPROVED_SCOPE_FRESH_ACTUAL_PARENT_STARTUP_REQUIRED','phase_memory_bytes':dict(PHASE_MEMORY),
        'default_probe_controls_memory_bytes':DEFAULT_MEMORY,'owner_approval_sha256':OWNER_APPROVAL_SHA256,
        'actual_parent_startup_guard_sha256':ACTUAL_PARENT_STARTUP_GUARD_SHA256,
        'scan_effective_kernel_memory_bytes':SCAN_KERNEL_MEMORY,'required_memory_page_size_bytes':PAGE_SIZE,
        'required_observed_host_parent_headroom_bytes':REQUIRED_HOST_PARENT_HEADROOM,
        'freeform_resource_values_permitted':False,'resource_expansion_authorized':True,
        'source_readiness_and_actual_execution_acceptance_inferred':False}
