**work in progress. nothing works yet**

Что уже есть:
+ функция, которая шедулит под так же, как это делал бы настоящий кубернетес, но без запуска binding-плагинов.

Что делать дальше:
+ написать свой binding-плагин или симитировать биндинг другим способом.

Как запустить то, что есть:
```
git clone https://github.com/kubernetes/kubernetes $KUBE_DIR
cp -r * $KUBE_DIR
cd $KUBE_DIR
go run pkg/scratch/main.go -v=10
```
