Decoding BE301 433MHz curtain remotes.

They use a continuous-phase (differential) coded-mark-inversion encoding on a OOK modulation @ 433MHz.

Use this tool with rtl433:
rtl_433 -R 0 -f 433.92M -M hires -M level -w OOK:- | ./rtl_433-cmi_decode 1>>data.dump

License: BSD