-module(fib).
-export([main/0]).
fib(0) -> 0;
fib(1) -> 1;
fib(N) -> fib(N-1) + fib(N-2).
main() -> io:format("~p~n", [fib(35)]).
