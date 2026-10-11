"""Bounded local subprocess capture for the inert producer; no network methods."""
import os
import selectors
import signal
import subprocess
import time
from osv_completion import IncompleteScan

STDOUT_LIMIT = 16*1024*1024
STDERR_LIMIT = 4*1024*1024
AGGREGATE_LIMIT = 128*1024*1024


class Budget:
    def __init__(self, seconds=1800, limit=AGGREGATE_LIMIT):
        self.deadline=time.monotonic()+seconds
        self.limit=limit
        self.used=0
    def left(self):
        remaining=self.deadline-time.monotonic()
        if remaining<=0:raise IncompleteScan('overall producer deadline exceeded')
        return remaining
    def charge(self, count):
        self.used+=count
        if self.used>self.limit:raise IncompleteScan('aggregate producer output limit exceeded')


def bounded_run(argv, cwd, env, timeout, budget=None, stdout_limit=STDOUT_LIMIT, stderr_limit=STDERR_LIMIT):
    """Read both pipes incrementally, kill only this owned process group on failure."""
    budget=Budget() if budget is None else budget
    end=time.monotonic()+min(timeout,budget.left())
    proc=None;reaped=False;selector=selectors.DefaultSelector();buffers=[bytearray(),bytearray()]
    try:
        proc=subprocess.Popen(argv,cwd=cwd,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
                              start_new_session=True)
        for index,pipe in enumerate([proc.stdout,proc.stderr]):
            os.set_blocking(pipe.fileno(),False);selector.register(pipe,selectors.EVENT_READ,index)
        while selector.get_map():
            remaining=min(end-time.monotonic(),budget.left())
            if remaining<=0:raise IncompleteScan('producer process deadline exceeded')
            for key,_ in selector.select(min(remaining,0.25)):
                chunk=os.read(key.fileobj.fileno(),65536)
                if not chunk:selector.unregister(key.fileobj);continue
                index=key.data
                if len(buffers[index])+len(chunk)>[stdout_limit,stderr_limit][index]:
                    raise IncompleteScan('producer stream size limit exceeded')
                budget.charge(len(chunk));buffers[index].extend(chunk)
        remaining=min(end-time.monotonic(),budget.left())
        if remaining<=0:raise IncompleteScan('producer process deadline exceeded')
        rc=proc.wait(timeout=remaining)
        reaped=True
        result=subprocess.CompletedProcess(argv,rc,bytes(buffers[0]).decode('utf-8'),bytes(buffers[1]).decode('utf-8'))
        return result
    except (OSError,UnicodeError,subprocess.TimeoutExpired) as error:
        raise IncompleteScan('producer process failed capture or deadline') from error
    finally:
        selector.close()
        if proc is not None:
            if not reaped:
                try:os.killpg(proc.pid,signal.SIGKILL)
                except ProcessLookupError:pass
                proc.wait(timeout=5)
            for pipe in [proc.stdout,proc.stderr]:
                if pipe is not None:pipe.close()

