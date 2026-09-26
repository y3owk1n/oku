package shim

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tie makes Windows end p when the shim ends, as a program ends when it is
// stopped on macOS and Linux. The job's handle closes with the shim, which ends
// every process in the job. Processes that p starts stay out of the job, so a
// launcher that starts a program and exits leaves that program running. Where
// Windows refuses the job, p runs as before.
func tie(p *os.Process) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK

	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job) //nolint:errcheck

		return
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid),
	)
	if err != nil {
		windows.CloseHandle(job) //nolint:errcheck

		return
	}
	defer windows.CloseHandle(process) //nolint:errcheck

	if windows.AssignProcessToJobObject(job, process) != nil {
		windows.CloseHandle(job) //nolint:errcheck
	}
}
