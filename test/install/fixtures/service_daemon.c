/* Native stand-in so the service controller can verify /proc/PID/exe. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv)
{
    if (argc > 1 && strcmp(argv[1], "-dry-run") == 0)
        return 0;

#ifdef FAIL_START
    return 1;
#else
    const char *log = getenv("FIXTURE_PIDS");
    if (log == NULL)
        return 1;
    FILE *pids = fopen(log, "a");
    if (pids == NULL)
        return 1;
    fprintf(pids, "%ld\n", (long)getpid());
    if (fclose(pids) != 0)
        return 1;
    for (;;)
        pause();
#endif
}
