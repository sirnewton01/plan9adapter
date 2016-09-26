# Introduction

Plan 9 doesn't have an extensive set of device drivers. However, it does have a set of conventions
for accessing different kinds of devices, such as networking, system clock, cpu statistics and
even plain filesystem through a protocol called 9P, which allows access to these services over
a network.

This project exposes services available through a computer running on a platform supported by
Go (http://www.golang.org) as a 9P server. The goal is to allow remote access via Plan 9 to the
non-native systems.
